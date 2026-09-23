package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/nextpay"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// NextPay 真实服务 UAT（seam 之后的盲区专用）
//
// 仓库里其它所有 checkout 测试都替换 createNextpayCheckoutFn，因此从不接触
// NextPay 的真实 HTTP 契约，也从不验证 payment_method 是否被 Stripe 接受。
// 本文件里的测试**故意不替换任何 seam**：直接调 createNextpayCheckout /
// ensureOrderCheckout，打一台真实 NextPay，再向真实 Stripe 回读它建出的
// Checkout Session。
//
// 默认跳过（需要本地 NextPay + Stripe 测试密钥）。开启方式见
// scripts/uat/nextpay-router-uat.sh，它会把环境搭好并设置：
//
//	NEXTPAY_UAT=1 go test -count=1 -run TestUAT ./...
//
// 不加 build tag 是故意的：本文件在每次普通 go test 下仍然参与编译，
// 生产代码改签名时会立刻编译失败，而不是等到有人想起来跑 UAT 才发现腐烂。
// ============================================================================

// skipUnlessNextpayUAT 要求显式开关 + 可用 config + 已配置的 nextpay 渠道。
func skipUnlessNextpayUAT(t *testing.T) {
	t.Helper()
	if os.Getenv("NEXTPAY_UAT") != "1" {
		t.Skip("NEXTPAY_UAT != 1 (需要本地 NextPay 实例，见 scripts/uat/nextpay-router-uat.sh)")
	}
	skipIfNoConfig(t)
	if !configNextpay(context.Background()).Ready() {
		t.Skip("nextpay 渠道未配置（缺 access_key / webhook_secret）")
	}
	// SDK 的 client 是 sync.Once 缓存的；显式指向本次 UAT 的实例，避免被别的测试先初始化。
	nextpay.SetConfig(&nextpay.Config{
		AccessKey: viper.GetString("nextpay.access_key"),
		Endpoint:  viper.GetString("nextpay.endpoint"),
		Timeout:   viper.GetInt("nextpay.timeout"),
	})
}

// uatUserWithEmail 建一个带邮箱登录身份的用户（createNextpayCheckout 要求邮箱）。
func uatUserWithEmail(t *testing.T) *User {
	t.Helper()
	u := CreateTestUser(t)
	u.Brand = string(BrandKaitu)
	require.NoError(t, db.Get().Model(u).Update("brand", u.Brand).Error)
	enc, err := secretEncryptString(context.Background(), fmt.Sprintf("uat-%d@kaitu.test", time.Now().UnixNano()))
	require.NoError(t, err)
	li := &LoginIdentify{UserID: u.ID, Type: "email",
		IndexID: fmt.Sprintf("uat-%d", time.Now().UnixNano()), EncryptedValue: enc, Brand: u.Brand}
	require.NoError(t, db.Get().Create(li).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(li) })
	return u
}

// uatOrder 落一张未付订单（ensureOrderCheckout 会 FOR UPDATE 重读它）。
func uatOrder(t *testing.T, u *User, plan *Plan) *Order {
	t.Helper()
	o := &Order{UUID: generateId("uat"), Title: plan.Label, OriginAmount: plan.Price,
		PayAmount: plan.Price, UserID: u.ID, IsPaid: BoolPtr(false)}
	require.NoError(t, o.SetOrderMeta(plan, nil, nil, true))
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	return o
}

// stripeSessionIDFromURL 从 checkout URL 里取 session id：
// https://checkout.stripe.com/c/pay/cs_test_xxx#fid...
func stripeSessionIDFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	for _, seg := range strings.Split(u.Path, "/") {
		if strings.HasPrefix(seg, "cs_") {
			return seg
		}
	}
	t.Fatalf("checkout URL 里找不到 cs_ session id: %s", raw)
	return ""
}

// uatStripeKey 从配置文件直接读 Stripe 密钥，**不走 viper**。
// 本包的测试共享 viper 全局状态，logic_stripe_test.go 的 t.Cleanup 会把
// stripe.secret_key 置空 —— 依赖 viper 会让本测试单跑绿、全量红（顺序依赖）。
func uatStripeKey(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../center/config.yml")
	require.NoError(t, err, "需要 ../center/config.yml 才能回读 Stripe session")
	var cfg struct {
		Stripe struct {
			SecretKey string `yaml:"secret_key"`
		} `yaml:"stripe"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &cfg))
	return strings.TrimSpace(cfg.Stripe.SecretKey)
}

// stripeGetSession 直接打 Stripe REST 回读 session（不经 SDK，避免版本形状纠缠）。
func stripeGetSession(t *testing.T, sessionID string) map[string]any {
	t.Helper()
	key := uatStripeKey(t)
	require.NotEmpty(t, key, "配置里没有 stripe.secret_key，无法回读 session")
	require.True(t, strings.HasPrefix(key, "sk_test"), "UAT 只允许 Stripe 测试密钥，拒绝用 live key 跑测试")

	req, err := http.NewRequest("GET", "https://api.stripe.com/v1/checkout/sessions/"+sessionID, nil)
	require.NoError(t, err)
	req.SetBasicAuth(key, "")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, 200, resp.StatusCode, "stripe 回读 session 失败: %v", out)
	return out
}

// TestUAT_NextpayCheckout_RealServiceContract 是"seam 之后"那段的唯一真实验证：
// 真实 createNextpayCheckout → 真实 NextPay（建单 + confirm）→ 真实 Stripe Session。
func TestUAT_NextpayCheckout_RealServiceContract(t *testing.T) {
	skipUnlessNextpayUAT(t)
	require.NoError(t, Migrate())

	u := uatUserWithEmail(t)
	plan := &Plan{PID: "uat-app-1m", Label: "开途 UAT 套餐", Price: 1999, Month: 1, Product: ProductApp}
	o := uatOrder(t, u, plan)

	co, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err, "真实 NextPay 建单+confirm 必须成功")
	require.NotNil(t, co)

	assert.NotEmpty(t, co.NextpayOrderID, "NextPay 必须回 orderId —— 这是 SDK 反序列化字段名是否对得上的判据")
	assert.True(t, strings.HasPrefix(co.CheckoutURL, "https://checkout.stripe.com/"),
		"payUrl 必须是 Stripe 托管收银台直链（客户端只 openExternal 不解析），实际=%s", co.CheckoutURL)

	// 回读 Stripe：确认金额/币种/回跳地址真的按我们传的参数落到了 Stripe 侧。
	sess := stripeGetSession(t, stripeSessionIDFromURL(t, co.CheckoutURL))
	assert.EqualValues(t, o.PayAmount, sess["amount_total"], "Stripe 侧金额必须等于订单应付额")
	assert.Equal(t, "usd", sess["currency"])
	assert.Equal(t, "payment", sess["mode"], "一次性购买必须是 payment 模式，不能是 subscription")
	assert.Equal(t, "open", sess["status"])
	successURL, _ := sess["success_url"].(string)
	assert.Contains(t, successURL, "/pay-result/"+o.UUID,
		"成功回跳必须落到官网 pay-result 页并带订单 uuid")
	assert.Contains(t, successURL, "session_id={CHECKOUT_SESSION_ID}",
		"NextPay 会在 success_url 后拼 session_id，payResultURL 因此不能自带 query")
}

// TestUAT_NextpayCheckout_PaymentMethodMore 验证 configNextpay 的默认 payment_method
// 在这个 Stripe 账号上真的可用，并把它实际列出的支付方式断言下来。
// 这条测试存在的理由：spec 说 "more 在该账号从未用过"，唯一的消除方式是真的建一次。
func TestUAT_NextpayCheckout_PaymentMethodMore(t *testing.T) {
	skipUnlessNextpayUAT(t)
	require.NoError(t, Migrate())

	require.Equal(t, "more", configNextpay(context.Background()).PaymentMethod,
		"本测试锚定缺省渠道 more；若刻意改成 alipay，请同步改这里的断言")

	u := uatUserWithEmail(t)
	plan := &Plan{PID: "uat-more", Label: "开途 UAT more", Price: 39900, Month: 12, Product: ProductApp}
	o := uatOrder(t, u, plan)

	co, err := createNextpayCheckout(context.Background(), u, o, plan)
	require.NoError(t, err, "payment_method=more 必须被 NextPay 与 Stripe 同时接受")

	sess := stripeGetSession(t, stripeSessionIDFromURL(t, co.CheckoutURL))
	raw, _ := sess["payment_method_types"].([]any)
	methods := make([]string, 0, len(raw))
	for _, m := range raw {
		if s, ok := m.(string); ok {
			methods = append(methods, s)
		}
	}
	t.Logf("more 在本账号实际列出的支付方式: %v", methods)

	assert.Contains(t, methods, "card", "more 至少要给出银行卡")
	// 中国大陆用户的两条主力通道。它们在则不需要回退到显式 alipay。
	assert.Contains(t, methods, "alipay", "more 必须包含支付宝，否则大陆用户只剩银行卡，应回退 nextpay.payment_method=alipay")
	assert.Contains(t, methods, "wechat_pay", "more 必须包含微信支付")
}

// TestUAT_EnsureOrderCheckout_ReuseThenRebuild 用真实服务验证复用窗口与整单重建：
// 23h 内复用同一 Stripe session；超窗则在 NextPay 侧另建一张订单。
func TestUAT_EnsureOrderCheckout_ReuseThenRebuild(t *testing.T) {
	skipUnlessNextpayUAT(t)
	require.NoError(t, Migrate())

	u := uatUserWithEmail(t)
	plan := &Plan{PID: "uat-reuse", Label: "开途 UAT 复用", Price: 2999, Month: 1, Product: ProductApp}
	o := uatOrder(t, u, plan)

	first, err := ensureOrderCheckout(context.Background(), db.Get(), u, o, plan)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	firstNP := o.NextpayOrderID
	require.NotEmpty(t, firstNP, "首次必须落 nextpay_order_id")
	assert.Equal(t, OrderChannelNextpay, o.Channel, "首次必须把订单渠道标成 nextpay")

	// 落库校验：三列真的写进去了（Select 白名单是否覆盖到位）。
	var persisted Order
	require.NoError(t, db.Get().First(&persisted, o.ID).Error)
	assert.Equal(t, firstNP, persisted.NextpayOrderID)
	assert.Equal(t, OrderChannelNextpay, persisted.Channel)
	gotURL, gotAt := persisted.GetCheckout()
	assert.Equal(t, first, gotURL, "checkoutUrl 必须落进 Meta")
	assert.NotZero(t, gotAt)
	assert.Equal(t, payRedirectURL(o.UUID), persisted.GetPayUrl(),
		"Meta.payUrl 必须是耐久 302 链接，不是 Stripe 直链")

	// 窗口内复用：不应该再建 NextPay 订单。
	second, err := ensureOrderCheckout(context.Background(), db.Get(), u, &persisted, plan)
	require.NoError(t, err)
	assert.Equal(t, first, second, "23h 窗口内必须复用缓存的 Stripe session")
	assert.Equal(t, firstNP, persisted.NextpayOrderID, "复用时不得新建 NextPay 订单")

	// 超窗：把 checkoutAt 推到 24h 前，必须整单重建且拿到不同的 NextPay 订单。
	require.NoError(t, persisted.SetOrderCheckout(payRedirectURL(persisted.UUID), gotURL,
		time.Now().Add(-24*time.Hour).Unix()))
	require.NoError(t, db.Get().Model(&persisted).Select("Meta").Updates(&persisted).Error)

	third, err := ensureOrderCheckout(context.Background(), db.Get(), u, &persisted, plan)
	require.NoError(t, err)
	assert.NotEqual(t, first, third, "超过复用窗口必须换新的 Stripe session")
	assert.NotEqual(t, firstNP, persisted.NextpayOrderID,
		"NextPay 订单 30 分钟后拒 confirm，超窗必须整单重建")
}

// TestUAT_EnsureOrderCheckout_AlreadyPaidDropsSession 复现终审抓到的那个双付缺口的防线：
// NextPay 往返期间订单被并发 webhook 入账 → 新 session 必须被丢弃，且不得覆盖 is_paid。
func TestUAT_EnsureOrderCheckout_AlreadyPaidDropsSession(t *testing.T) {
	skipUnlessNextpayUAT(t)
	require.NoError(t, Migrate())

	u := uatUserWithEmail(t)
	plan := &Plan{PID: "uat-paid-race", Label: "开途 UAT 竞态", Price: 1000, Month: 1, Product: ProductApp}
	o := uatOrder(t, u, plan)

	// 模拟"快照读到未付，但库里已经被 webhook 标成已付"。
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", o.ID).
		Updates(map[string]any{"is_paid": true, "paid_at": time.Now()}).Error)
	o.IsPaid = BoolPtr(false) // 调用方持有的旧快照

	_, err := ensureOrderCheckout(context.Background(), db.Get(), u, o, plan)
	require.ErrorIs(t, err, errOrderAlreadyPaid, "已付订单必须回 errOrderAlreadyPaid，绝不能落新 session")

	var after Order
	require.NoError(t, db.Get().First(&after, o.ID).Error)
	require.NotNil(t, after.IsPaid)
	assert.True(t, *after.IsPaid, "重建失败路径绝不能把 is_paid 洗回未付")
}

// TestUAT_EnsureOrderCheckout_WriteIsColumnScoped 直接证明落库写的是**列白名单**而不是整行：
// 终审抓到的 Critical 就是整行 Save 会把并发 webhook 刚提交的 is_paid/paid_at 洗回旧快照值，
// 让第二个 Stripe session 二次入账。这里用一个不在白名单里的列（title）当探针 —— 整行 Save
// 会把库里的值覆盖成调用方手里的陈旧值，列级 Updates 不会。
func TestUAT_EnsureOrderCheckout_WriteIsColumnScoped(t *testing.T) {
	skipUnlessNextpayUAT(t)
	require.NoError(t, Migrate())

	u := uatUserWithEmail(t)
	plan := &Plan{PID: "uat-scoped", Label: "开途 UAT 列作用域", Price: 1500, Month: 1, Product: ProductApp}
	o := uatOrder(t, u, plan)

	_, err := ensureOrderCheckout(context.Background(), db.Get(), u, o, plan)
	require.NoError(t, err)

	// 模拟"另一个事务在我们持有快照之后改了别的列"。
	const dbWins = "DB-WINS-并发提交的值"
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", o.ID).Update("title", dbWins).Error)
	o.Title = "STALE-调用方手里的陈旧值"

	// 把复用窗口推过期，强制走重建（= 真正会落库的那条路径）。
	url, _ := o.GetCheckout()
	require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), url, time.Now().Add(-24*time.Hour).Unix()))
	require.NoError(t, db.Get().Model(o).Select("Meta").Updates(o).Error)

	_, err = ensureOrderCheckout(context.Background(), db.Get(), u, o, plan)
	require.NoError(t, err)

	var after Order
	require.NoError(t, db.Get().First(&after, o.ID).Error)
	assert.Equal(t, dbWins, after.Title,
		"落库必须只写 NextpayOrderID/Channel/Meta 三列；整行 Save 会用陈旧快照覆盖并发提交的列（双付根因）")
	assert.NotEmpty(t, after.NextpayOrderID, "白名单内的列仍必须被写入")
}
