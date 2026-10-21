package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/wordgate/qtoolkit/log"
)

// 支付宝提现账户
//
// 收款信息（支付宝账号 + 真实姓名）是个人身份信息，只以 AES-256-GCM 密文存进
// WithdrawAccount.AccountSecret / Withdraw.AccountSecret（secretEncrypt，密钥
// SECRET_KEY_HASH 由进程启动时注入）。注意不要改用 secretEncryptString——它目前是明文直通。
//
// AccountID 列有全局唯一索引，而 GCM 每次随机 nonce、密文不可比较，所以支付宝的
// AccountID 存 HMAC-SHA256(SECRET_KEY_HASH, user_id + 账号)，只用于本人去重，从不展示：
//   - 带密钥：手机号空间只有 ~10^10，无密钥哈希（如 secretHashIt 的源码常量盐）库一泄露即可穷举反推；
//   - 带 user_id：唯一性落在本人维度，别的账号既无法探测某支付宝号是否已被绑定，也无法抢先占用。
// 用户侧接口返回前用 maskWithdrawAccountForUser / maskWithdrawForUser 换成脱敏值；
// 只有管理端提现列表（打款要用）解密出明文。

const (
	alipayAccountIDPrefix = "alipay:"
	alipayAccountMaxLen   = 100
	alipayRealNameMaxLen  = 30 // 字符数
)

var alipayPhonePattern = regexp.MustCompile(`^1[0-9]{10}$`)

// alipayPayee 是加密前的明文结构
type alipayPayee struct {
	Account  string `json:"account"`
	RealName string `json:"realName"`
}

// normalizeAlipayAccount 去空格；邮箱统一小写（支付宝邮箱账号不区分大小写），保证哈希去重稳定
func normalizeAlipayAccount(account string) string {
	account = strings.TrimSpace(account)
	if strings.Contains(account, "@") {
		account = strings.ToLower(account)
	}
	return account
}

// validateAlipayPayee 校验支付宝账号（大陆手机号或邮箱）与真实姓名，入参须已规范化
func validateAlipayPayee(account, realName string) error {
	if account == "" {
		return fmt.Errorf("支付宝账号不能为空")
	}
	if len(account) > alipayAccountMaxLen {
		return fmt.Errorf("支付宝账号过长")
	}
	if !alipayPhonePattern.MatchString(account) && !isValidEmail(account) {
		return fmt.Errorf("支付宝账号格式不正确，请填写手机号或邮箱")
	}
	if realName == "" {
		return fmt.Errorf("请填写支付宝实名认证的真实姓名")
	}
	if utf8.RuneCountInString(realName) > alipayRealNameMaxLen {
		return fmt.Errorf("真实姓名过长")
	}
	return nil
}

// alipayAccountID 返回用于唯一索引去重的确定性标识（入参账号须已规范化）
func alipayAccountID(ctx context.Context, userID uint64, account string) (string, error) {
	key, err := getGlobalSecretKey(ctx)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "withdraw-alipay:%d:%s", userID, account)
	return alipayAccountIDPrefix + hex.EncodeToString(mac.Sum(nil)), nil
}

// encryptAlipayPayee 加密收款信息，返回 base64(nonce|ciphertext|tag)
func encryptAlipayPayee(ctx context.Context, p alipayPayee) (string, error) {
	plain, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	enc, err := secretEncrypt(ctx, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// decryptAlipayPayee 解密 encryptAlipayPayee 的结果
func decryptAlipayPayee(ctx context.Context, secret string) (alipayPayee, error) {
	var p alipayPayee
	if secret == "" {
		return p, fmt.Errorf("empty alipay secret")
	}
	enc, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return p, err
	}
	plain, err := secretDecrypt(ctx, enc)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(plain, &p)
	return p, err
}

// maskAlipayAccount 脱敏展示：手机号 138****5678，邮箱沿用 hideEmail
func maskAlipayAccount(account string) string {
	if alipayPhonePattern.MatchString(account) {
		return account[:3] + "****" + account[7:]
	}
	if isValidEmail(account) {
		return hideEmail(account)
	}
	return "****"
}

// maskRealName 只露姓氏：张三 → 张*，欧阳娜娜 → 欧***
func maskRealName(name string) string {
	runes := []rune(name)
	if len(runes) <= 1 {
		return name
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1)
}

// alipayDisplayForUser 给用户侧展示的脱敏标识；解不开时（密钥轮换等）退化为固定占位，绝不回落到哈希
func alipayDisplayForUser(ctx context.Context, secret string) string {
	p, err := decryptAlipayPayee(ctx, secret)
	if err != nil {
		log.Errorf(ctx, "failed to decrypt alipay payee for display: %v", err)
		return "****"
	}
	return maskAlipayAccount(p.Account) + " (" + maskRealName(p.RealName) + ")"
}

// maskWithdrawAccountForUser 把支付宝账户的 AccountID（哈希）换成脱敏展示值；其他渠道原样返回
func maskWithdrawAccountForUser(ctx context.Context, a *WithdrawAccount) {
	if a == nil || a.AccountType != WithdrawAccountTypeAlipay {
		return
	}
	a.AccountID = alipayDisplayForUser(ctx, a.AccountSecret)
}

// maskWithdrawForUser 同上，作用于提现申请的快照及其关联账户
func maskWithdrawForUser(ctx context.Context, w *Withdraw) {
	if w == nil {
		return
	}
	if w.AccountType == WithdrawAccountTypeAlipay {
		w.AccountID = alipayDisplayForUser(ctx, w.AccountSecret)
	}
	maskWithdrawAccountForUser(ctx, w.WithdrawAccount)
}

// adminWithdrawAccountResource 管理端提现列表的收款信息：支付宝解密出明文账号与真实姓名供打款
func adminWithdrawAccountResource(ctx context.Context, w *Withdraw) ResourceWithdrawAccount {
	res := ResourceWithdrawAccount{
		AccountType: string(w.AccountType),
		AccountID:   w.AccountID,
		Currency:    string(w.Currency),
	}
	if w.AccountType != WithdrawAccountTypeAlipay {
		return res
	}
	p, err := decryptAlipayPayee(ctx, w.AccountSecret)
	if err != nil {
		log.Errorf(ctx, "failed to decrypt alipay payee for withdraw %d: %v", w.ID, err)
		res.AccountID = "（解密失败）"
		return res
	}
	res.AccountID = p.Account
	res.RealName = p.RealName
	return res
}
