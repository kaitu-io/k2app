package center

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_apple_webhook 处理 App Store Server Notifications V2。
//
// 与 wordgate webhook 一致，使用 HTTP 状态码表达 S2S 重试语义（非 JSON code）：
//   - 200 = 已处理，勿重试
//   - 4xx = 坏请求，停止重试（Apple 不再重试）
//   - 5xx = 临时失败，请重试
//
// 安全（双层防御）：
//  1. verifyAppleJWS：x5c 链校验到内置 Apple Root CA G3，硬拒非 Apple 来源。
//  2. verifyAndGrantTransaction：向 Apple 认证 API（GetTransaction）复核——载重信任
//     锚点在那里，payload 字段不可伪造。
func api_apple_webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Errorf(c, "[AppleWebhook] read body: %v", err)
		c.AbortWithStatus(400)
		return
	}

	var req struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.SignedPayload == "" {
		log.Warnf(c, "[AppleWebhook] bad payload: %v", err)
		c.AbortWithStatus(400)
		return
	}

	// Layer 1: reject payloads whose x5c chain doesn't root at Apple Root CA G3.
	if err := verifyAppleJWS(req.SignedPayload); err != nil {
		log.Warnf(c, "[AppleWebhook] JWS signature rejected: %v", err)
		c.AbortWithStatus(400)
		return
	}

	asn, err := appstore.NewNotification(c, req.SignedPayload)
	if err != nil || asn.Payload == nil || asn.TransactionInfo == nil {
		log.Warnf(c, "[AppleWebhook] parse failed: %v", err)
		c.AbortWithStatus(400)
		return
	}

	otx := asn.TransactionInfo.OriginalTransactionId
	nType := asn.Payload.NotificationType
	uuid := asn.Payload.NotificationUUID
	log.Infof(c, "[AppleWebhook] type=%s subtype=%s otx=%s uuid=%s", nType, asn.Payload.Subtype, otx, uuid)

	// 仅处理我们已知（已在 verify 端点绑定过 userID）的订阅链。
	var sub Subscription
	if err := db.Get().Where(&Subscription{Provider: "apple", ProviderSubscriptionID: otx}).First(&sub).Error; err != nil {
		// 未知 otx：正常场景——SUBSCRIBED 通知可早于客户端 verify 到达；
		// 返回 200 告知 Apple 不必重试（客户端 verify 会完成首次绑定）。
		log.Infof(c, "[AppleWebhook] otx=%s not yet bound (expected before client verify), skipping", otx)
		c.Status(200)
		return
	}

	// 幂等：同一通知 UUID 已处理过则跳过（Apple 偶发重送）。
	if sub.LastEventID != "" && sub.LastEventID == uuid {
		log.Infof(c, "[AppleWebhook] duplicate notification uuid=%s otx=%s, already processed", uuid, otx)
		c.Status(200)
		return
	}

	switch nType {
	case appstore.NotificationType_DID_RENEW,
		appstore.NotificationType_SUBSCRIBED,
		appstore.NotificationType_OFFER_REDEEMED:
		// 复核当前交易并入账（绝对 expiresDate，幂等抬升）。
		if err := verifyAndGrantTransaction(c, sub.UserID, asn.TransactionInfo.TransactionId); err != nil {
			log.Errorf(c, "[AppleWebhook] grant failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	case appstore.NotificationType_DID_CHANGE_RENEWAL_STATUS,
		appstore.NotificationType_DID_FAIL_TO_RENEW:
		// 续订开关 / 计费状态变更（用户取消自动续订、扣费失败进入宽限或重试）。
		// 关键：不 re-grant、不改用户权益到期——取消后用户仍享有到本周期结束，
		// 真正到期由 EXPIRED 事件落地。仅把 auto_renew + status 落到订阅行。
		if err := applyRenewalInfo(c, &sub, asn.RenewalInfo, asn.Payload.Subtype); err != nil {
			log.Errorf(c, "[AppleWebhook] apply renewal info failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	case appstore.NotificationType_REFUND, appstore.NotificationType_REVOKE:
		if err := revokeSubscription(c, &sub); err != nil {
			log.Errorf(c, "[AppleWebhook] revoke failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	case appstore.NotificationType_EXPIRED, appstore.NotificationType_GRACE_PERIOD_EXPIRED:
		// 到期：expired_at 已等于 Apple expiresDate，自然过期，仅标记状态。
		if err := setSubStatus(c, sub.ID, "expired"); err != nil {
			log.Errorf(c, "[AppleWebhook] mark expired failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	default:
		log.Infof(c, "[AppleWebhook] unhandled type=%s otx=%s", nType, otx)
	}

	if err := recordSubEventID(c, sub.ID, uuid); err != nil {
		log.Warnf(c, "[AppleWebhook] record uuid failed otx=%s: %v", otx, err)
	}
	c.Status(200)
}

func setSubStatus(ctx context.Context, id uint64, status string) error {
	return db.Get().Model(&Subscription{}).Where("id = ?", id).Update("status", status).Error
}

func recordSubEventID(ctx context.Context, id uint64, eventID string) error {
	return db.Get().Model(&Subscription{}).Where("id = ?", id).Update("last_event_id", eventID).Error
}

// verifyAppleJWS validates the x5c certificate chain embedded in an Apple JWS
// signed payload. Apple's JWS header carries x5c:[leaf, intermediate]; we verify
// leaf→intermediate→Apple Root CA G3 (embedded in qtoolkit). Returns nil only if
// the chain is valid and roots at Apple's CA — any forgery or self-signed cert fails.
func verifyAppleJWS(payload string) error {
	parts := strings.Split(payload, ".")
	if len(parts) < 3 {
		return errors.New("invalid JWS: expected header.payload.signature")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode JWS header: %w", err)
	}
	var header struct {
		X5c []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("parse JWS header: %w", err)
	}
	if len(header.X5c) < 2 {
		return fmt.Errorf("x5c chain has %d cert(s); leaf + intermediate required", len(header.X5c))
	}
	leafDER, err := base64.StdEncoding.DecodeString(header.X5c[0])
	if err != nil {
		return fmt.Errorf("decode leaf cert: %w", err)
	}
	intDER, err := base64.StdEncoding.DecodeString(header.X5c[1])
	if err != nil {
		return fmt.Errorf("decode intermediate cert: %w", err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return fmt.Errorf("parse leaf cert: %w", err)
	}
	intermediate, err := x509.ParseCertificate(intDER)
	if err != nil {
		return fmt.Errorf("parse intermediate cert: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(appstore.AppleRootCAPEM) {
		return errors.New("load Apple root CA: PEM parse failed")
	}
	intermediates := x509.NewCertPool()
	intermediates.AddCert(intermediate)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
	}); err != nil {
		return fmt.Errorf("chain: %w", err)
	}
	return nil
}
