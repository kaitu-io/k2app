package center

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// APNs 通道（token-based .p8 认证）
// =====================================================================
//
// config.yml:
//
//	apns:
//	  team_id: NJT954Q3RH
//	  key_id:  XXXXXXXXXX
//	  p8: |               # 二选一：内联 PEM
//	    -----BEGIN PRIVATE KEY-----
//	    ...
//	  p8_path: /etc/kaitu/AuthKey_XXXX.p8   # 或文件路径
//
// 一把 key 覆盖团队下所有 App（开途 / Overleap 同属 NJT954Q3RH）；topic 按 token 的 bundle 区分。

// apnsNotification 一次 APNs 请求的全部输入
type apnsNotification struct {
	Token   string
	Topic   string
	Sandbox bool
	Title   string
	Body    string
	Route   string
	PushID  uint64
}

// apnsResult APNs 的响应；Err 非 nil = 网络层失败（没拿到 HTTP 响应）。
type apnsResult struct {
	StatusCode int
	Reason     string
	Err        error
}

func (r apnsResult) ok() bool { return r.Err == nil && r.StatusCode == http.StatusOK }

func (r apnsResult) isBadDeviceToken() bool {
	return r.Err == nil && r.Reason == apns2.ReasonBadDeviceToken
}

// isDeadToken token 本身已不可用：卸载 / 过期 / 不属于这个 topic / 格式不对。停用，不重试。
func (r apnsResult) isDeadToken() bool {
	if r.Err != nil {
		return false
	}
	if r.StatusCode == http.StatusGone {
		return true
	}
	switch r.Reason {
	case apns2.ReasonUnregistered, apns2.ReasonBadDeviceToken, apns2.ReasonDeviceTokenNotForTopic,
		apns2.ReasonExpiredToken, apns2.ReasonMissingDeviceToken:
		return true
	}
	return false
}

// isAuthError 服务端凭据 / 配置错误：每个 token 都会一样失败，必须告警，不是 token 的问题。
func (r apnsResult) isAuthError() bool {
	if r.Err != nil {
		return false
	}
	if r.StatusCode == http.StatusForbidden {
		return true
	}
	switch r.Reason {
	case apns2.ReasonBadTopic, apns2.ReasonTopicDisallowed, apns2.ReasonMissingTopic,
		apns2.ReasonBadCertificate, apns2.ReasonBadCertificateEnvironment:
		return true
	}
	return false
}

func (r apnsResult) describe() string {
	if r.Err != nil {
		return "network: " + r.Err.Error()
	}
	return fmt.Sprintf("%d %s", r.StatusCode, r.Reason)
}

// apnsSender 发送抽象，测试用假实现替换。
type apnsSender interface {
	Send(ctx context.Context, n apnsNotification) apnsResult
}

// apnsTokenClient 真实实现：production / development 两个 HTTP/2 客户端共用一个 provider token。
type apnsTokenClient struct {
	prod *apns2.Client
	dev  *apns2.Client
}

func (c *apnsTokenClient) Send(ctx context.Context, n apnsNotification) apnsResult {
	p := payload.NewPayload().AlertTitle(n.Title).AlertBody(n.Body).Sound("default").
		Custom("push_id", n.PushID)
	if n.Route != "" {
		p = p.Custom("route", n.Route)
	}
	notif := &apns2.Notification{
		DeviceToken: n.Token,
		Topic:       n.Topic,
		Payload:     p,
		PushType:    apns2.PushTypeAlert,
		Priority:    apns2.PriorityHigh,
	}
	client := c.prod
	if n.Sandbox {
		client = c.dev
	}
	res, err := client.PushWithContext(ctx, notif)
	if err != nil {
		return apnsResult{Err: err}
	}
	return apnsResult{StatusCode: res.StatusCode, Reason: res.Reason}
}

// initAPNs 从配置构建 apnsClient。缺配置 / 配置坏 = 打 Error 并保持 nil：
// PushToUser 随后返回 ErrPushChannelUnavailable，调用方能回退，绝不静默假成功。
func initAPNs() {
	ctx := context.Background()
	teamID := strings.TrimSpace(viper.GetString("apns.team_id"))
	keyID := strings.TrimSpace(viper.GetString("apns.key_id"))
	p8 := viper.GetString("apns.p8")
	p8Path := viper.GetString("apns.p8_path")

	if teamID == "" || keyID == "" || (p8 == "" && p8Path == "") {
		log.Errorf(ctx, "[PUSH] APNs not configured (apns.team_id / key_id / p8|p8_path) — iOS push disabled, PushToUser returns ErrPushChannelUnavailable")
		apnsClient = nil
		return
	}

	var (
		authKey *ecdsa.PrivateKey
		err     error
	)
	if p8 != "" {
		authKey, err = token.AuthKeyFromBytes([]byte(p8))
	} else {
		authKey, err = token.AuthKeyFromFile(p8Path)
	}
	if err != nil {
		log.Errorf(ctx, "[PUSH] APNs auth key invalid: %v — iOS push disabled", err)
		apnsClient = nil
		return
	}

	tk := &token.Token{AuthKey: authKey, KeyID: keyID, TeamID: teamID}
	apnsClient = &apnsTokenClient{
		prod: apns2.NewTokenClient(tk).Production(),
		dev:  apns2.NewTokenClient(tk).Development(),
	}
	log.Infof(ctx, "[PUSH] APNs channel ready (team=%s key=%s)", teamID, keyID)
}
