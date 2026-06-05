package center

import (
	"context"
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_apple_webhook 处理 App Store Server Notifications V2。
//
// 与 wordgate webhook 一致，使用 HTTP 状态码表达 S2S 重试语义（非 JSON code）：
//   - 200 = 已处理，勿重试
//   - 4xx = 坏请求，停止重试
//   - 5xx = 临时失败，请重试
//
// 安全：payload 视为不可信触发器。只读 originalTransactionId + 通知类型，再由
// verifyAndGrantTransaction 向 Apple 认证 API（GetTransaction）复核后入账——载重信任锚点
// 在那里，而非这里的签名解析。伪造 webhook 命中不存在的交易(Apple 404→no-op)或真交易
// (字段不可篡改)，均无法越权发放权益。
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
		// 未知 originalTransactionId（如 SUBSCRIBED 早于已鉴权 verify 到达）→ 忽略，Apple 不重试。
		log.Infof(c, "[AppleWebhook] unknown originalTransactionId=%s, ignoring", otx)
		c.Status(200)
		return
	}

	// 幂等：同一通知 UUID 已处理过则跳过。
	if sub.LastEventID != "" && sub.LastEventID == uuid {
		log.Debugf(c, "[AppleWebhook] duplicate notification uuid=%s, skipping", uuid)
		c.Status(200)
		return
	}

	switch nType {
	case appstore.NotificationType_DID_RENEW,
		appstore.NotificationType_SUBSCRIBED,
		appstore.NotificationType_OFFER_REDEEMED,
		appstore.NotificationType_DID_CHANGE_RENEWAL_STATUS:
		// 复核当前交易并入账（绝对 expiresDate，幂等抬升）。
		if err := verifyAndGrantTransaction(c, sub.UserID, asn.TransactionInfo.TransactionId); err != nil {
			log.Errorf(c, "[AppleWebhook] grant failed otx=%s: %v", otx, err)
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
