package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 访客聊天令牌与主体解析。
// 令牌 = base64url(json).base64url(HMAC-SHA256)；签名密钥 = HMAC-SHA256(jwt.secret, purpose)，
// 用途不同密钥不同，所以 ws 令牌与 resume 令牌互相不能冒充。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md §5/§6

const (
	chatWSPurpose     = "chat-ws-v1"
	chatResumePurpose = "chat-resume-v1"

	chatWSTokenTTL     = 5 * time.Minute
	chatResumeTokenTTL = 7 * 24 * time.Hour
)

var errChatToken = errors.New("invalid chat token")

// chatTokenClaims 是令牌载荷。P 是用途，与密钥派生双重绑定。
type chatTokenClaims struct {
	P string `json:"p"`
	B string `json:"b,omitempty"` // ws：品牌
	K string `json:"k,omitempty"` // ws：主体类型
	I uint64 `json:"i,omitempty"` // ws：主体 ID
	C string `json:"c,omitempty"` // resume：会话 UUID
	E int64  `json:"e"`           // 过期 unix 秒
}

// chatTokenKey 由 jwt.secret 按用途派生；secret 未配置返回 nil（签发/校验都拒绝）。
func chatTokenKey(purpose string) []byte {
	secret := configJwt(context.Background()).Secret
	if secret == "" {
		return nil
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(purpose))
	return m.Sum(nil)
}

func signChatToken(purpose string, claims chatTokenClaims, ttl time.Duration) string {
	key := chatTokenKey(purpose)
	if key == nil {
		return ""
	}
	claims.P = purpose
	claims.E = time.Now().Add(ttl).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return ""
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func parseChatToken(purpose, tok string, now time.Time) (chatTokenClaims, error) {
	var claims chatTokenClaims
	key := chatTokenKey(purpose)
	if key == nil {
		return claims, errChatToken
	}
	body, sig, ok := strings.Cut(tok, ".")
	if !ok || body == "" || sig == "" {
		return claims, errChatToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return claims, errChatToken
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(body))
	if !hmac.Equal(got, m.Sum(nil)) {
		return claims, errChatToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return claims, errChatToken
	}
	if claims.P != purpose || now.Unix() >= claims.E {
		return chatTokenClaims{}, errChatToken
	}
	return claims, nil
}

// signChatWSToken 签发 WebSocket 接入令牌（空串 = jwt.secret 未配置）。
func signChatWSToken(s chatSubject, ttl time.Duration) string {
	return signChatToken(chatWSPurpose, chatTokenClaims{B: string(s.Brand), K: s.Kind, I: s.ID}, ttl)
}

func parseChatWSToken(tok string, now time.Time) (chatSubject, error) {
	c, err := parseChatToken(chatWSPurpose, tok, now)
	if err != nil {
		return chatSubject{}, err
	}
	if c.B == "" || (c.K != SubjectGuest && c.K != SubjectUser) || c.I == 0 {
		return chatSubject{}, errChatToken
	}
	return chatSubject{Brand: Brand(c.B), Kind: c.K, ID: c.I}, nil
}

// signChatResumeToken 签发邮件里的"继续对话"链接令牌。
func signChatResumeToken(convUUID string, ttl time.Duration) string {
	return signChatToken(chatResumePurpose, chatTokenClaims{C: convUUID}, ttl)
}

func parseChatResumeToken(tok string, now time.Time) (convUUID string, err error) {
	c, err := parseChatToken(chatResumePurpose, tok, now)
	if err != nil {
		return "", err
	}
	if c.C == "" {
		return "", errChatToken
	}
	return c.C, nil
}

// ---- 请求 → 主体 ----

const (
	// CookieChatCid 访客设备标识：22 字符 base64url，属性同 sid。
	CookieChatCid = "cid"
	chatCidMaxAge = funnelSidMaxAge
)

// 限流：每 IP 每分钟。session 会建 guest（30），消息/邮箱（60）。
var (
	chatSessionLimiter = newFunnelIPLimiter(30)
	chatMessageLimiter = newFunnelIPLimiter(60)
)

func setChatCidCookie(c *gin.Context, value string) {
	isSecure := c.GetHeader("X-Forwarded-Proto") == "https" || c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieChatCid, value, chatCidMaxAge, "/", "", isSecure, true)
}

// chatAcceptLocale 取 Accept-Language 的首个语言标签（截到 guests.locale 的长度）。
func chatAcceptLocale(c *gin.Context) string {
	v := c.GetHeader("Accept-Language")
	if v == "" {
		return ""
	}
	tag, _, _ := strings.Cut(v, ",")
	tag, _, _ = strings.Cut(tag, ";")
	return funnelTruncate(strings.TrimSpace(tag), 16)
}

// chatSubjectFromRequest 从请求得到会话主体。
// 已登录（funnelSilentUserID>0，已校验品牌一致）→ user；否则按 cid cookie 找 guest 簇根。
// allowCreate：无合法 cid 时种 cookie，并按 cid 新建 guest（含 sid 归并）；
// 否则只读：cid 缺失 / 本品牌下不存在都返回 false，不种 cookie、不写库。
func chatSubjectFromRequest(c *gin.Context, allowCreate bool) (chatSubject, bool) {
	brand := ReqBrand(c)
	if uid := funnelSilentUserID(c); uid > 0 {
		return chatSubject{Brand: brand, Kind: SubjectUser, ID: uid}, true
	}
	ctx := c.Request.Context()
	cid, _ := c.Cookie(CookieChatCid)
	if !validFunnelSid(cid) {
		cid = ""
	}
	if !allowCreate {
		if cid == "" {
			return chatSubject{}, false
		}
		owner, err := findIdentityOwner(ctx, brand, IdentityCID, cid)
		if err != nil || owner == nil {
			return chatSubject{}, false
		}
		root, err := guestRootID(ctx, owner.GuestID)
		if err != nil {
			return chatSubject{}, false
		}
		return chatSubject{Brand: brand, Kind: SubjectGuest, ID: root}, true
	}
	if cid == "" {
		cid = newFunnelSid()
		setChatCidCookie(c, cid)
	}
	sid := ""
	if !funnelGPC(c) {
		if v, optedOut := readFunnelSid(c); !optedOut {
			sid = v
		}
	}
	root, err := resolveGuest(ctx, brand, cid, sid, chatAcceptLocale(c), funnelTruncate(CountryFromGinContext(c), 2))
	if err != nil {
		return chatSubject{}, false
	}
	return chatSubject{Brand: brand, Kind: SubjectGuest, ID: root}, true
}
