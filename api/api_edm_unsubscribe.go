package center

import (
	"bytes"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// EDM 一键退订页（无需登录，令牌即凭证，见 logic_edm_unsubscribe.go）。
//
//	GET  /api/edm/unsubscribe?t=<token> → 确认页（带按钮）。GET 绝不退订：邮箱的链接扫描 / 预取会打 GET。
//	POST /api/edm/unsubscribe?t=<token> → 退订并返回「已退订」页。兼容 RFC 8058 一键退订
//	     （邮箱商 POST，body = List-Unsubscribe=One-Click；只需 2xx）。
//
// Always-200 的例外：返回 HTML 页而非 JSON，用真实 HTTP 状态（无效令牌 400，内部错误 500），
// 与 asynqmon 同类。无效令牌页不区分「签名错 / 用户不存在」，不泄露任何信息。
// 挂在 /api 下而非 /app：overleap.io 只反代 /api/*（sites/overleap/next.config.ts），/app 在该域名不可达。

type edmUnsubCopy struct {
	Lang         string
	ConfirmTitle string
	ConfirmBody  string
	Button       string
	DoneTitle    string
	DoneBody     string
	InvalidTitle string
	InvalidBody  string
	ErrorTitle   string
	ErrorBody    string
}

// 文案里的品牌名 / 客服邮箱一律取自 BrandConfig（{{.BrandName}} / {{.SupportEmail}}），不写字面量。
var edmUnsubCopyKaitu = edmUnsubCopy{
	Lang:         "zh-CN",
	ConfirmTitle: "退订{{.BrandName}}营销邮件",
	ConfirmBody:  "确认后，你将不再收到{{.BrandName}}的活动与优惠邮件。登录验证码等账户安全邮件不受影响。",
	Button:       "确认退订",
	DoneTitle:    "已退订",
	DoneBody:     "你已退订{{.BrandName}}营销邮件，之后不会再收到此类邮件。",
	InvalidTitle: "链接无效",
	InvalidBody:  "这个退订链接无效或不完整。请直接点击邮件里的链接，或发邮件到 {{.SupportEmail}} 联系我们。",
	ErrorTitle:   "暂时无法退订",
	ErrorBody:    "系统出了点问题，请稍后再试，或发邮件到 {{.SupportEmail}} 联系我们。",
}

var edmUnsubCopyOverleap = edmUnsubCopy{
	Lang:         "en",
	ConfirmTitle: "Unsubscribe from {{.BrandName}} marketing emails",
	ConfirmBody:  "Once you confirm, you will no longer receive promotional emails from {{.BrandName}}. Sign-in codes and other account security emails are not affected.",
	Button:       "Unsubscribe",
	DoneTitle:    "You're unsubscribed",
	DoneBody:     "You have been unsubscribed from {{.BrandName}} marketing emails.",
	InvalidTitle: "Invalid link",
	InvalidBody:  "This unsubscribe link is invalid or incomplete. Please use the link in the email, or contact us at {{.SupportEmail}}.",
	ErrorTitle:   "Something went wrong",
	ErrorBody:    "We couldn't process your request. Please try again later, or contact us at {{.SupportEmail}}.",
}

func edmUnsubCopyFor(b Brand) edmUnsubCopy {
	if b == BrandOverleap {
		return edmUnsubCopyOverleap
	}
	return edmUnsubCopyKaitu
}

var edmUnsubPageTmpl = template.Must(template.New("edm-unsub").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Title}} · {{.BrandName}}</title>
<style>body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;background:#f5f6f8;color:#1f2329}
main{max-width:480px;margin:12vh auto 0;padding:32px 24px;background:#fff;border-radius:12px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
h1{font-size:20px;margin:0 0 12px}p{line-height:1.6;margin:0 0 20px;color:#4e5969}
button{font-size:16px;padding:10px 24px;border:0;border-radius:8px;background:#1f2329;color:#fff;cursor:pointer}</style>
</head><body><main><h1>{{.Title}}</h1><p>{{.Body}}</p>{{if .Token}}<form method="post" action="?t={{.Token}}"><button type="submit">{{.Button}}</button></form>{{end}}</main></body></html>`))

type edmUnsubPage struct {
	Lang, BrandName, Title, Body, Button, Token string
}

// edmUnsubFill 把文案里的 {{.BrandName}} / {{.SupportEmail}} 用品牌注册表填好（文案是受信常量）。
func edmUnsubFill(s string, cfg *BrandConfig) string {
	t, err := template.New("c").Parse(s)
	if err != nil {
		return s
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, map[string]string{"BrandName": cfg.DisplayName, "SupportEmail": cfg.SupportEmail}); err != nil {
		return s
	}
	return buf.String()
}

func renderEDMUnsubPage(c *gin.Context, status int, b Brand, title, body, button, token string) {
	cfg := b.Config()
	cp := edmUnsubCopyFor(b)
	page := edmUnsubPage{
		Lang:      cp.Lang,
		BrandName: cfg.DisplayName,
		Title:     edmUnsubFill(title, cfg),
		Body:      edmUnsubFill(body, cfg),
		Button:    button,
		Token:     token,
	}
	var buf bytes.Buffer
	if err := edmUnsubPageTmpl.Execute(&buf, page); err != nil {
		log.Errorf(c, "edm unsubscribe: render page: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	// 令牌在 URL 里：不缓存、不外泄 Referer、不收录。
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex")
	c.Data(status, "text/html; charset=utf-8", buf.Bytes())
}

// edmUnsubResolve 校验令牌并取出用户（只读 id + brand + 退订状态）。失败返回 nil。
func edmUnsubResolve(c *gin.Context) (*User, string) {
	tok := c.Query("t")
	if tok == "" {
		tok = c.PostForm("t")
	}
	uid, err := parseEDMUnsubscribeToken(tok)
	if err != nil {
		return nil, ""
	}
	var u User
	if err := db.Get().Select("id", "brand", "edm_opt_out").First(&u, uid).Error; err != nil {
		return nil, ""
	}
	return &u, tok
}

func renderEDMUnsubInvalid(c *gin.Context) {
	cp := edmUnsubCopyFor(ReqBrand(c))
	renderEDMUnsubPage(c, http.StatusBadRequest, ReqBrand(c), cp.InvalidTitle, cp.InvalidBody, "", "")
}

// api_edm_unsubscribe_page GET：只展示确认页，不改任何状态。
func api_edm_unsubscribe_page(c *gin.Context) {
	u, tok := edmUnsubResolve(c)
	if u == nil {
		renderEDMUnsubInvalid(c)
		return
	}
	b := Brand(u.Brand)
	cp := edmUnsubCopyFor(b)
	if u.EdmOptOut {
		renderEDMUnsubPage(c, http.StatusOK, b, cp.DoneTitle, cp.DoneBody, "", "")
		return
	}
	renderEDMUnsubPage(c, http.StatusOK, b, cp.ConfirmTitle, cp.ConfirmBody, cp.Button, tok)
}

// api_edm_unsubscribe POST：退订（幂等）。表单按钮与 RFC 8058 一键退订共用。
func api_edm_unsubscribe(c *gin.Context) {
	u, _ := edmUnsubResolve(c)
	if u == nil {
		renderEDMUnsubInvalid(c)
		return
	}
	b := Brand(u.Brand)
	cp := edmUnsubCopyFor(b)
	if !u.EdmOptOut {
		if err := optOutEDM(c.Request.Context(), u.ID, time.Now()); err != nil {
			log.Errorf(c, "edm unsubscribe: opt out user %d: %v", u.ID, err)
			renderEDMUnsubPage(c, http.StatusInternalServerError, b, cp.ErrorTitle, cp.ErrorBody, "", "")
			return
		}
		log.Infof(c, "edm unsubscribe: user %d opted out (one_click=%t)", u.ID, c.PostForm("List-Unsubscribe") == "One-Click")
	}
	renderEDMUnsubPage(c, http.StatusOK, b, cp.DoneTitle, cp.DoneBody, "", "")
}
