package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

const alipayTestKey = "0123456789abcdef0123456789abcdef" // 32 字节，AES-256

func TestAlipayPayee_EncryptRoundTrip(t *testing.T) {
	t.Setenv("SECRET_KEY_HASH", alipayTestKey)
	ctx := context.Background()
	p := alipayPayee{Account: "13812345678", RealName: "张三"}

	s1, err := encryptAlipayPayee(ctx, p)
	require.NoError(t, err)
	s2, err := encryptAlipayPayee(ctx, p)
	require.NoError(t, err)

	// 随机 nonce：同一明文两次密文不同；密文（含 base64 解码后）不含任何明文片段
	assert.NotEqual(t, s1, s2)
	raw, err := base64.StdEncoding.DecodeString(s1)
	require.NoError(t, err)
	for _, needle := range []string{"13812345678", "张三", "realName"} {
		assert.NotContains(t, s1, needle)
		assert.NotContains(t, string(raw), needle)
	}
	// 最长输入也放得进 varchar(512)
	long, err := encryptAlipayPayee(ctx, alipayPayee{
		Account:  strings.Repeat("a", alipayAccountMaxLen-8) + "@x.com",
		RealName: strings.Repeat("龘", alipayRealNameMaxLen),
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(long), 512)

	got, err := decryptAlipayPayee(ctx, s1)
	require.NoError(t, err)
	assert.Equal(t, p, got)

	// 换密钥解不开（GCM 认证失败），不会返回垃圾明文
	t.Setenv("SECRET_KEY_HASH", "fedcba9876543210fedcba9876543210")
	_, err = decryptAlipayPayee(ctx, s1)
	assert.Error(t, err)
	_, err = decryptAlipayPayee(ctx, "")
	assert.Error(t, err)
}

func TestAlipayPayee_Validate(t *testing.T) {
	cases := []struct {
		account, name string
		ok            bool
	}{
		{"13812345678", "张三", true},
		{"user@example.com", "李四", true},
		{"", "张三", false},
		{"13812345678", "", false},
		{"23812345678", "张三", false},  // 非 1 开头
		{"1381234567", "张三", false},   // 10 位
		{"138123456789", "张三", false}, // 12 位
		{"not-an-account", "张三", false},
		{"13812345678", strings.Repeat("名", alipayRealNameMaxLen+1), false},
		{strings.Repeat("a", alipayAccountMaxLen) + "@x.com", "张三", false},
	}
	for _, tc := range cases {
		err := validateAlipayPayee(tc.account, tc.name)
		assert.Equal(t, tc.ok, err == nil, "%q / %q: %v", tc.account, tc.name, err)
	}
}

func TestAlipayAccountID_DeterministicAndNormalized(t *testing.T) {
	ctx := context.Background()
	a := alipayAccountID(ctx, normalizeAlipayAccount("  User@Example.COM "))
	b := alipayAccountID(ctx, normalizeAlipayAccount("user@example.com"))
	assert.Equal(t, a, b, "邮箱大小写/空白不同应视为同一账户")
	assert.True(t, strings.HasPrefix(a, alipayAccountIDPrefix))
	assert.LessOrEqual(t, len(a), 100, "必须放得进 account_id varchar(100)")
	assert.NotContains(t, a, "example")
	assert.NotEqual(t, a, alipayAccountID(ctx, "13812345678"))
}

func TestAlipayMasking(t *testing.T) {
	assert.Equal(t, "138****5678", maskAlipayAccount("13812345678"))
	assert.Equal(t, "use*@e******.com", maskAlipayAccount("user@example.com"))
	assert.Equal(t, "张*", maskRealName("张三"))
	assert.Equal(t, "欧***", maskRealName("欧阳娜娜"))

	t.Setenv("SECRET_KEY_HASH", alipayTestKey)
	ctx := context.Background()
	secret, err := encryptAlipayPayee(ctx, alipayPayee{Account: "13812345678", RealName: "张三"})
	require.NoError(t, err)

	acct := &WithdrawAccount{AccountType: WithdrawAccountTypeAlipay, AccountID: "alipay:hash", AccountSecret: secret}
	w := &Withdraw{AccountType: WithdrawAccountTypeAlipay, AccountID: "alipay:hash", AccountSecret: secret, WithdrawAccount: acct}
	maskWithdrawForUser(ctx, w)
	assert.Equal(t, "138****5678 (张*)", w.AccountID)
	assert.Equal(t, "138****5678 (张*)", acct.AccountID)

	// 其他渠道原样
	pp := &WithdrawAccount{AccountType: WithdrawAccountTypePayPal, AccountID: "a@b.com"}
	maskWithdrawAccountForUser(ctx, pp)
	assert.Equal(t, "a@b.com", pp.AccountID)

	// 管理端拿明文
	res := adminWithdrawAccountResource(ctx, &Withdraw{AccountType: WithdrawAccountTypeAlipay, AccountID: "alipay:hash", AccountSecret: secret, Currency: CurrencyCNY})
	assert.Equal(t, ResourceWithdrawAccount{AccountType: "alipay", AccountID: "13812345678", Currency: "cny", RealName: "张三"}, res)

	// 解不开时绝不回落到哈希或密文
	broken := &WithdrawAccount{AccountType: WithdrawAccountTypeAlipay, AccountID: "alipay:hash", AccountSecret: "garbage"}
	maskWithdrawAccountForUser(ctx, broken)
	assert.Equal(t, "****", broken.AccountID)
}

// 接线守卫：载荷含支付宝明文的两条路由只能挂在不记 body 的组上。
// log.MiddlewareRequestLog(true) 会把 POST 请求体与 JSON 响应体原样写进 app.log。
func TestAlipayRoutes_NotBodyLogged(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	s := string(src)

	assert.Contains(t, s, `walletSensitive.POST("/withdraw-accounts", AuthRequired(), WalletRequired(), api_create_withdraw_account)`)
	assert.Contains(t, s, `adminSensitive.GET("/wallet/withdraws", api_admin_list_withdraw_requests)`)
	assert.Equal(t, 1, strings.Count(s, "api_create_withdraw_account"), "不得在别的组重复注册")
	assert.Equal(t, 1, strings.Count(s, "api_admin_list_withdraw_requests"), "不得在别的组重复注册")
	for _, g := range []string{"walletSensitive", "adminSensitive"} {
		use := regexp.MustCompile(g + `\.Use\(([^\n]*)\)`).FindStringSubmatch(s)
		require.NotNil(t, use, g)
		assert.Contains(t, use[1], "requestLogWithoutBodies()", g)
		assert.NotContains(t, use[1], "MiddlewareRequestLog", g)
	}
}

// 真库全链路：添加 → 去重 → 删除重填 → 他人占用 → 提现 → 用户侧脱敏 / 管理端解密
func TestAlipayWithdraw_EndToEnd(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	t.Setenv("SECRET_KEY_HASH", alipayTestKey)
	r := SetupRouter()

	user, key := createBrandIsoAccessKeyUser(t, BrandKaitu, false)
	other, otherKey := createBrandIsoAccessKeyUser(t, BrandKaitu, false)
	_, adminKey := createBrandIsoAccessKeyUser(t, BrandKaitu, true)
	t.Cleanup(func() {
		for _, uid := range []uint64{user.ID, other.ID} {
			var wallet Wallet
			if db.Get().Where(&Wallet{UserID: uid}).First(&wallet).Error == nil {
				db.Get().Unscoped().Where("wallet_id = ?", wallet.ID).Delete(&WalletChange{})
				db.Get().Unscoped().Delete(&wallet)
			}
			db.Get().Unscoped().Where("user_id = ?", uid).Delete(&Withdraw{})
			db.Get().Unscoped().Where("user_id = ?", uid).Delete(&WithdrawAccount{})
		}
	})

	phone := fmt.Sprintf("139%08d", rand.Intn(100000000)) // 共享测试库：每次换号避免撞残留行
	masked := phone[:3] + "****" + phone[7:]

	call := func(method, path, k string, body any) *TestResponse {
		t.Helper()
		req := NewTestRequest(method, path).WithHeader("X-Access-Key", k)
		if body != nil {
			req = req.WithBody(body)
		}
		resp, err := ParseResponse(req.Execute(r))
		require.NoError(t, err)
		return resp
	}
	addAlipay := func(k, account, name string) *TestResponse {
		return call("POST", "/api/wallet/withdraw-accounts", k, map[string]any{
			"accountType": "alipay", "accountId": account, "currency": "cny", "realName": name,
		})
	}

	// 1. 添加：响应只给脱敏值，库里只有哈希与密文
	resp := addAlipay(key, " "+phone+" ", " 王小二 ")
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.NotContains(t, string(resp.Data), phone)
	assert.NotContains(t, string(resp.Data), "王小二")
	assert.NotContains(t, string(resp.Data), "accountSecret")
	assert.Contains(t, string(resp.Data), masked+" (王**)")

	var row WithdrawAccount
	require.NoError(t, db.Get().Where(&WithdrawAccount{UserID: user.ID}).First(&row).Error)
	assert.Equal(t, WithdrawAccountTypeAlipay, row.AccountType)
	assert.Equal(t, CurrencyCNY, row.Currency, "支付宝强制人民币")
	assert.True(t, strings.HasPrefix(row.AccountID, alipayAccountIDPrefix))
	assert.NotContains(t, row.AccountID+row.AccountSecret, phone)
	payee, err := decryptAlipayPayee(context.Background(), row.AccountSecret)
	require.NoError(t, err)
	assert.Equal(t, alipayPayee{Account: phone, RealName: "王小二"}, payee)

	// 2. 校验失败：缺真实姓名
	resp = addAlipay(key, phone, "")
	assert.Equal(t, 422, resp.Code)

	// 3. 本人重复添加
	resp = addAlipay(key, phone, "王小二")
	assert.Equal(t, 422, resp.Code)

	// 4. 删掉后用同一账号重填正确姓名：就地恢复（以前会撞唯一索引报 500）
	resp = call("DELETE", fmt.Sprintf("/api/wallet/withdraw-accounts/%d", row.ID), key, nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	resp = addAlipay(key, phone, "王晓二")
	require.Equal(t, 0, resp.Code, resp.Message)
	var restored WithdrawAccount
	require.NoError(t, db.Get().Where(&WithdrawAccount{UserID: user.ID}).First(&restored).Error)
	assert.Equal(t, row.ID, restored.ID)
	payee, err = decryptAlipayPayee(context.Background(), restored.AccountSecret)
	require.NoError(t, err)
	assert.Equal(t, "王晓二", payee.RealName)
	assert.True(t, *restored.IsDefault, "唯一账户恢复后应为默认")

	// 5. 别的账号绑同一支付宝：明确 422，不是 500
	resp = addAlipay(otherKey, phone, "王晓二")
	assert.Equal(t, 422, resp.Code, resp.Message)

	// 6. 列表脱敏
	resp = call("GET", "/api/wallet/withdraw-accounts", key, nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.NotContains(t, string(resp.Data), phone)
	assert.Contains(t, string(resp.Data), masked+" (王**)")

	// 7. 提现：快照带密文、手续费 0、响应脱敏
	require.NoError(t, db.Get().Create(&Wallet{UserID: user.ID, Balance: 5000, TotalIncome: 5000}).Error)
	resp = call("POST", "/api/wallet/withdraws", key, map[string]any{"amount": 3000, "withdrawAccountId": row.ID})
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.NotContains(t, string(resp.Data), phone)
	var wd Withdraw
	require.NoError(t, db.Get().Where(&Withdraw{UserID: user.ID}).First(&wd).Error)
	assert.Equal(t, restored.AccountSecret, wd.AccountSecret)
	assert.Equal(t, CurrencyCNY, wd.Currency)
	assert.EqualValues(t, 0, wd.FeeAmount)
	assert.EqualValues(t, 3000, wd.NetAmount)

	resp = call("GET", "/api/wallet/withdraws", key, nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.NotContains(t, string(resp.Data), phone)
	assert.Contains(t, string(resp.Data), masked+" (王**)")

	// 8. 账户删除后，管理端仍能从提现快照解出明文打款
	require.NoError(t, db.Get().Delete(&restored).Error)
	resp = call("GET", "/app/wallet/withdraws?status=pending&pageSize=100", adminKey, nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	var list ListResult[AdminWithdrawListItem]
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	var found *AdminWithdrawListItem
	for i := range list.Items {
		if list.Items[i].ID == wd.ID {
			found = &list.Items[i]
		}
	}
	require.NotNil(t, found, "admin list should contain withdraw %d", wd.ID)
	assert.Equal(t, ResourceWithdrawAccount{AccountType: "alipay", AccountID: phone, Currency: "cny", RealName: "王晓二"}, found.Account)
}
