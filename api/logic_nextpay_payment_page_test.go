package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/nextpay"
	"gorm.io/gorm"
)

// setViperForTest 设置 viper 键，结束后还原成原值（本包共享 viper，见 api/CLAUDE.md Test Convention）。
func setViperForTest(t *testing.T, key string, val any) {
	t.Helper()
	orig := viper.Get(key)
	t.Cleanup(func() { viper.Set(key, orig) })
	viper.Set(key, val)
}

// fakeNextpay 起一个假的 NextPay，记录 confirm 被调了几次。
// 真实 createNextpayCheckout 经 qtoolkit SDK 打它——不替换任何 seam。
type fakeNextpay struct {
	srv          *httptest.Server
	confirmCalls atomic.Int32
	paymentURL   string
	lastMetadata atomic.Value // string：最近一次建单请求的 metadata 原文
}

func newFakeNextpay(t *testing.T, paymentURL string) *fakeNextpay {
	t.Helper()
	f := &fakeNextpay{paymentURL: paymentURL}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/checkout/order":
			var body struct {
				Metadata string `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.lastMetadata.Store(body.Metadata)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"orderId": "np-ord-x", "paymentUrl": f.paymentURL, "amount": 4990, "currency": "usd"}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/confirm"):
			f.confirmCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"checkoutUrl": "https://checkout.stripe.com/c/pay/cs_fake"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	// SDK 初始化优先读 viper：把 endpoint 指向假服务，再 SetConfig 让 sync.Once 重跑。
	setViperForTest(t, "nextpay.endpoint", f.srv.URL)
	setViperForTest(t, "nextpay.access_key", "test-ak")
	nextpay.SetConfig(nil)
	t.Cleanup(func() { nextpay.SetConfig(nil) })
	return f
}

func TestNextpayPaymentPageBucket_Bounds(t *testing.T) {
	for i := 0; i < 500; i++ {
		uuid := fmt.Sprintf("user-%d", i)
		assert.False(t, nextpayPaymentPageBucket(uuid, 0), "0%% 必须全部走 Stripe Checkout")
		assert.True(t, nextpayPaymentPageBucket(uuid, 100), "100%% 必须全部走支付页")
		assert.False(t, nextpayPaymentPageBucket(uuid, -5), "负数按 0 处理")
		assert.True(t, nextpayPaymentPageBucket(uuid, 150), "超过 100 按 100 处理")
	}
}

func TestNextpayPaymentPageBucket_StableAndProportional(t *testing.T) {
	const n = 20000
	hit := 0
	for i := 0; i < n; i++ {
		uuid := fmt.Sprintf("user-%d.abc%d", i, i*7)
		a := nextpayPaymentPageBucket(uuid, 20)
		require.Equal(t, a, nextpayPaymentPageBucket(uuid, 20), "同一用户必须固定落在同一组")
		if a {
			hit++
		}
		// 单调：20% 组里的用户在 50% 时也必须在组里（放量不会把已在新路径的人切回去）。
		if a {
			require.True(t, nextpayPaymentPageBucket(uuid, 50))
		}
	}
	ratio := float64(hit) / n
	assert.InDelta(t, 0.20, ratio, 0.02, "20%% 灰度实际命中 %.3f", ratio)
}

func TestCreateNextpayCheckout_PaymentPageMode_SkipsConfirm(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	fake := newFakeNextpay(t, "https://pay.arbella.group/pay/np-ord-x")
	setViperForTest(t, "nextpay.payment_page_percent", 100)
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)
	o := uatOrder(t, u, plan)

	co, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err)
	assert.Equal(t, "https://pay.arbella.group/pay/np-ord-x", co.CheckoutURL)
	assert.Equal(t, "np-ord-x", co.NextpayOrderID)
	assert.Equal(t, checkoutModePaymentPage, co.Mode)
	assert.Equal(t, int32(0), fake.confirmCalls.Load(), "支付页模式不得调用 ConfirmPayment（confirm 会锁定支付方式）")
}

func TestCreateNextpayCheckout_StripeCheckoutMode_Confirms(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	fake := newFakeNextpay(t, "https://pay.arbella.group/pay/np-ord-x")
	setViperForTest(t, "nextpay.payment_page_percent", 0)
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)
	o := uatOrder(t, u, plan)

	co, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_fake", co.CheckoutURL)
	assert.Equal(t, checkoutModeStripeCheckout, co.Mode)
	assert.Equal(t, int32(1), fake.confirmCalls.Load())
}

func TestCreateNextpayCheckout_PaymentPageMode_EmptyURLFails(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	newFakeNextpay(t, "")
	setViperForTest(t, "nextpay.payment_page_percent", 100)
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)
	o := uatOrder(t, u, plan)

	_, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.Error(t, err, "空 paymentUrl 不能当成功返回给用户")
}

// ensureOrderCheckout 的复用窗口按缓存链接的路径区分：支付页 25 分钟（NextPay 订单 30 分钟后拒付），
// Stripe Checkout 23 小时。老订单 Meta 里没有 mode 字段，按 Stripe Checkout 处理。
func TestEnsureOrderCheckout_ReuseWindowByMode(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	var rebuilt int
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		rebuilt++
		return &nextpayCheckout{NextpayOrderID: "np-new", CheckoutURL: "https://pay.arbella.group/pay/np-new", Mode: checkoutModePaymentPage}, nil
	}

	cases := []struct {
		name   string
		mode   string // "" = 老订单，没有 mode 字段
		age    time.Duration
		reused bool
	}{
		{"支付页 20 分钟内复用", checkoutModePaymentPage, 20 * time.Minute, true},
		{"支付页 26 分钟重建", checkoutModePaymentPage, 26 * time.Minute, false},
		{"Stripe Checkout 26 分钟仍复用", checkoutModeStripeCheckout, 26 * time.Minute, true},
		{"Stripe Checkout 24 小时重建", checkoutModeStripeCheckout, 24 * time.Hour, false},
		{"老订单无 mode 26 分钟仍复用", "", 26 * time.Minute, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rebuilt = 0
			o := uatOrder(t, u, plan)
			cached := "https://cached.example/x"
			require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), cached, time.Now().Add(-tc.age).Unix()))
			if tc.mode != "" {
				require.NoError(t, o.SetCheckoutMode(tc.mode))
			}
			require.NoError(t, db.Get().Model(o).Update("meta", o.Meta).Error)

			var got string
			require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
				var err error
				got, err = ensureOrderCheckout(context.Background(), tx, u, o, plan)
				return err
			}))
			if tc.reused {
				assert.Equal(t, cached, got)
				assert.Equal(t, 0, rebuilt)
			} else {
				assert.Equal(t, "https://pay.arbella.group/pay/np-new", got)
				assert.Equal(t, 1, rebuilt)
			}
		})
	}
}

// 落库后 Meta 记下本单用的路径——灰度对比两组付款率靠它，不靠只留 3 天的 app.log。
func TestEnsureOrderCheckout_PersistsMode(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)
	o := uatOrder(t, u, plan)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		return &nextpayCheckout{NextpayOrderID: "np-1", CheckoutURL: "https://pay.arbella.group/pay/np-1", Mode: checkoutModePaymentPage}, nil
	}
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		_, err := ensureOrderCheckout(context.Background(), tx, u, o, plan)
		return err
	}))

	var saved Order
	require.NoError(t, db.Get().Where("id = ?", o.ID).First(&saved).Error)
	assert.Equal(t, checkoutModePaymentPage, saved.GetCheckoutMode())
	assert.Equal(t, payRedirectURL(o.UUID), saved.GetPayUrl(), "耐久链接不受影响")
	url, _ := saved.GetCheckout()
	assert.Equal(t, "https://pay.arbella.group/pay/np-1", url)
	_, err := saved.GetPlan()
	assert.NoError(t, err, "套餐快照不能被覆盖")
}

// 支付页链接追加来源参数；Stripe Checkout 链接（带 #fragment）原样返回，空值不加。
func TestWithPaySource(t *testing.T) {
	page := "https://pay.arbella.group/pay/np-1"
	got, err := url.Parse(withPaySource(page, checkoutModePaymentPage, "app", "windows", "0.4.14"))
	require.NoError(t, err)
	assert.Equal(t, "/pay/np-1", got.Path)
	assert.Equal(t, url.Values{"src": {"app"}, "platform": {"windows"}, "v": {"0.4.14"}}, got.Query())

	got, err = url.Parse(withPaySource(page+"?lang=zh", checkoutModePaymentPage, "mail", "", ""))
	require.NoError(t, err)
	assert.Equal(t, url.Values{"lang": {"zh"}, "src": {"mail"}}, got.Query(), "已有参数保留，空值不加")

	stripe := "https://checkout.stripe.com/c/pay/cs_1#fidkdWxOYHwnPyd1blpxYHZxWjA0"
	assert.Equal(t, stripe, withPaySource(stripe, checkoutModeStripeCheckout, "app", "windows", "0.4.14"))
	assert.Equal(t, stripe, withPaySource(stripe, "", "app", "windows", "0.4.14"), "老订单无 mode 按 Stripe Checkout 处理")
}

// seedPaidOrder 落一张已付订单（paid_at 指定），用于 firstPaidAt 口径测试。
func seedPaidOrder(t *testing.T, u *User, payAmount uint64, paidAt time.Time, refunded bool) {
	t.Helper()
	o := &Order{UUID: generateId("fpa"), Title: "fpa", OriginAmount: payAmount, PayAmount: payAmount,
		UserID: u.ID, IsPaid: BoolPtr(true), PaidAt: &paidAt, IsRefunded: BoolPtr(refunded), Meta: "{}"}
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
}

func nextpayMetadataOf(t *testing.T, f *fakeNextpay) map[string]any {
	t.Helper()
	raw, _ := f.lastMetadata.Load().(string)
	require.NotEmpty(t, raw, "建单请求必须带 metadata")
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	return m
}

// firstPaidAt 只取真钱首付：0 元单、已退款单都不算；值是 RFC3339 字符串；原有 brand/plan 字段保留。
func TestCreateNextpayCheckout_MetadataFirstPaidAt_EarliestRealPayment(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	fake := newFakeNextpay(t, "https://pay.arbella.group/pay/np-ord-x")
	setViperForTest(t, "nextpay.payment_page_percent", 100)
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)

	zeroAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	refundedAt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	realAt := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
	laterAt := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	seedPaidOrder(t, u, 0, zeroAt, false)       // 0 元 grant：不算
	seedPaidOrder(t, u, 2990, refundedAt, true) // 已退款：不算
	seedPaidOrder(t, u, 4990, laterAt, false)   // 真钱但更晚
	seedPaidOrder(t, u, 4990, realAt, false)    // 真钱首付
	o := uatOrder(t, u, plan)

	_, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err)
	m := nextpayMetadataOf(t, fake)
	assert.Equal(t, "2025-06-01T08:00:00Z", m["firstPaidAt"], "必须是 RFC3339 字符串，不能是数字")
	assert.Equal(t, string(BrandKaitu), m["brand"], "合并进 metadata，不能覆盖原有字段")
	assert.Equal(t, plan.PID, m["plan"])
}

// 没有真钱付款记录时整个键省略（不传 null / 空串）。
func TestCreateNextpayCheckout_MetadataFirstPaidAt_OmittedWithoutRealPayment(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	fake := newFakeNextpay(t, "https://pay.arbella.group/pay/np-ord-x")
	setViperForTest(t, "nextpay.payment_page_percent", 100)
	u := uatUserWithEmail(t)
	plan := nextpayTestPlan(t)
	seedPaidOrder(t, u, 0, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), false)
	o := uatOrder(t, u, plan) // 本单未付，也不能算

	_, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err)
	m := nextpayMetadataOf(t, fake)
	_, has := m["firstPaidAt"]
	assert.False(t, has, "无真钱付款记录时必须省略 firstPaidAt 键")
	assert.Equal(t, string(BrandKaitu), m["brand"])
}
