package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
)

// EDM 退订令牌与退订状态。spec 2026-11-04-email-winback-campaign-design §4.2
//
// 令牌 = base36(user_id) "." base64url(HMAC-SHA256(key, "edm-unsub|"+user_id)[:16])，
// key = chatTokenKey(edmUnsubscribePurpose)（由 jwt.secret 按用途派生，与聊天令牌互不可冒充）。
// 不含邮箱；不过期——邮件里的退订链接几个月后被点开也必须有效。令牌只能做一件事：让这一个用户退订。
// 签名段用 Strict 解码，避免同一签名有多种文本形式。

const (
	edmUnsubscribePurpose = "edm-unsubscribe-v1"
	edmUnsubscribePath    = "/api/edm/unsubscribe"
	// edmOptOutSkipReason 已退订用户在 email_send_logs 的 skipped 原因（error_msg）。
	edmOptOutSkipReason = "edm_opt_out"
	// edmUnsubscribeVar 每次 EDM 渲染都会注入的模板变量名。
	edmUnsubscribeVar = "UnsubscribeURL"
)

var errEDMUnsubscribeToken = errors.New("invalid edm unsubscribe token")

// edmOptOutExemptSlugs 走 EDM 通道、但属于服务通知（不是营销）的模板：已退订用户照发。
// 按模板 slug 而非请求参数做键：slug 是这些发送点在代码里的常量（下面直接引用常量，改名编译期跟着走），
// 已入队的 Asynq 任务也天然生效；请求级开关则任何调用方都能给任意批次打开，名单就失去意义。
// 决策（增长负责人）：只豁免专属线路/路由器欢迎装机信与流量滥用警告；续费提醒、试用召回 S1、
// 所有 winback / abandoned / recall 都不豁免。新增条目前先确认它确实不是营销。
var edmOptOutExemptSlugs = map[string]bool{
	privateNodeWelcomeSlug:  true, // 定制线路欢迎 / 装机引导
	routerWelcomeSlug:       true, // 路由器版欢迎 / 装机引导
	trafficAbuseWarningSlug: true, // 流量滥用警告
	billingIssueSlug:        true, // 扣费失败提醒（服务通知）
}

// inRecallHoldoutForEmail 召回邮件对照组判定：已退订 EDM 的用户两组都不进——他们在召回组会被
// skipped(edm_opt_out)，若留在对照组就只剩对照组有这批人，recall_stats 的对照组基线被拉偏。
// 只用于邮件；推送的对照组仍用 inRecallHoldout（退订邮件不影响推送）。
func inRecallHoldoutForEmail(user *User) bool {
	return !user.EdmOptOut && inRecallHoldout(user.ID)
}

func edmUnsubscribeMAC(key []byte, uid string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("edm-unsub|" + uid))
	return m.Sum(nil)[:16]
}

// signEDMUnsubscribeToken 空串 = jwt.secret 未配置或 userID 无效。
func signEDMUnsubscribeToken(userID uint64) string {
	key := chatTokenKey(edmUnsubscribePurpose)
	if key == nil || userID == 0 {
		return ""
	}
	uid := strconv.FormatUint(userID, 36)
	return uid + "." + base64.RawURLEncoding.EncodeToString(edmUnsubscribeMAC(key, uid))
}

func parseEDMUnsubscribeToken(tok string) (uint64, error) {
	key := chatTokenKey(edmUnsubscribePurpose)
	if key == nil {
		return 0, errEDMUnsubscribeToken
	}
	uid, sig, ok := strings.Cut(tok, ".")
	if !ok || uid == "" || sig == "" {
		return 0, errEDMUnsubscribeToken
	}
	got, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil || !hmac.Equal(got, edmUnsubscribeMAC(key, uid)) {
		return 0, errEDMUnsubscribeToken
	}
	id, err := strconv.ParseUint(uid, 36, 64)
	// 规范形式校验：ParseUint 接受大写/前导零，签名覆盖的是原串，这里拒掉非规范串保持令牌唯一形式。
	if err != nil || id == 0 || strconv.FormatUint(id, 36) != uid {
		return 0, errEDMUnsubscribeToken
	}
	return id, nil
}

// edmUnsubscribeURL 用户的退订链接，宿主取用户品牌的 BaseURL（∈ BrandConfig.Hosts，其 /api/* 反代到 Center）。
// 空串 = 无法签发。
func edmUnsubscribeURL(user *User) string {
	tok := signEDMUnsubscribeToken(user.ID)
	if tok == "" {
		return ""
	}
	base := strings.TrimRight(Brand(user.Brand).Config().BaseURL, "/")
	return base + edmUnsubscribePath + "?t=" + url.QueryEscape(tok)
}

// edmTemplateVars 每封 EDM 的渲染变量：调用方变量 + 系统变量 UnsubscribeURL（系统值覆盖同名调用方值）。
// 所有 EDM 都经 sendSingleTemplatedEmail 渲染，这里是唯一注入点——模板加退订页脚永远不会因缺变量发失败。
// 返回 false = 无法签发退订链接（jwt.secret 未配置），营销邮件宁可不发。
func edmTemplateVars(user *User, callerVars map[string]string) (map[string]string, bool) {
	u := edmUnsubscribeURL(user)
	if u == "" {
		return nil, false
	}
	vars := make(map[string]string, len(callerVars)+1)
	maps.Copy(vars, callerVars)
	vars[edmUnsubscribeVar] = u
	return vars, true
}

// optOutEDM 把用户标记为已退订 EDM。幂等：已退订的不改，保留首次退订时刻。
func optOutEDM(ctx context.Context, userID uint64, now time.Time) error {
	return db.Get().WithContext(ctx).Model(&User{}).
		Where("id = ? AND edm_opt_out = ?", userID, false).
		Updates(map[string]any{"edm_opt_out": true, "edm_opt_out_at": now.Unix()}).Error
}
