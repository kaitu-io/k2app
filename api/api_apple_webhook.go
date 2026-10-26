package center

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
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

	// 信任边界：Apple 根证书签的是所有 app 的通知，签名有效 ≠ 是本品牌 app 的通知。
	// 通知与交易的 bundleId 都必须是这条订阅所属品牌的 bundle；对不上一律 400（不重试）。
	var owner User
	if err := db.Get().Select("id", "brand", "apple_consumption_consent").First(&owner, sub.UserID).Error; err != nil {
		log.Errorf(c, "[AppleWebhook] load owner of otx=%s: %v", otx, err)
		c.AbortWithStatus(500)
		return
	}
	wantBundle := appleBundleIDForBrand(Brand(owner.Brand))
	if wantBundle == "" || asn.Payload.Data.BundleId != wantBundle ||
		(asn.TransactionInfo.BundleId != "" && asn.TransactionInfo.BundleId != wantBundle) {
		log.Warnf(c, "[AppleWebhook] bundle mismatch otx=%s: payload=%s txn=%s want=%s",
			otx, asn.Payload.Data.BundleId, asn.TransactionInfo.BundleId, wantBundle)
		c.AbortWithStatus(400)
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
		if !appleWebhookGrant(c, &sub, asn.TransactionInfo.TransactionId) {
			return
		}

	case appstore.NotificationType_DID_CHANGE_RENEWAL_PREF:
		// 只有 UPGRADE 立即生效（spec 2026-10-19 §3.9 立即转正）：Apple 当场扣费并附带新商品的交易，
		// 必须入账（客户端 verify 是主路径，这里兜底）。DOWNGRADE / 无 subtype 只是下个周期的续订偏好，
		// 到期时由 DID_RENEW 入账，这里不动。
		if asn.Payload.Subtype != appstore.Subtype_UPGRADE {
			log.Infof(c, "[AppleWebhook] renewal pref changed (subtype=%s) otx=%s, no credit", asn.Payload.Subtype, otx)
			break
		}
		if !appleWebhookGrant(c, &sub, asn.TransactionInfo.TransactionId) {
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
		revType, pct := decodeAppleRevocation(asn.Payload.Data.SignedTransactionInfo)
		if _, err := applyAppleRefund(c, otx, asn.TransactionInfo, appleRefundEvidence{
			SignedAt: asn.Payload.SignedDate, RevocationType: revType, RevocationPercentage: pct, Source: "webhook",
		}); err != nil {
			log.Errorf(c, "[AppleWebhook] refund failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	case appleNotificationRefundReversed:
		// 恢复时长是授予动作：与入账一样向 Apple 认证 API 复核（第二层信任锚点），
		// 只有 Apple 现在确认这笔交易不再是已退款，才恢复。
		info, err := fetchAppleTransaction(c, wantBundle, asn.TransactionInfo.TransactionId)
		if err != nil {
			log.Errorf(c, "[AppleWebhook] refund reversal re-verify failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}
		if info.TransactionId != asn.TransactionInfo.TransactionId || info.OriginalTransactionId != otx ||
			info.BundleId != wantBundle || info.RevocationDate != 0 {
			log.Warnf(c, "[AppleWebhook] refund reversal for txn %s not confirmed by Apple (revocationDate=%d), ignored",
				asn.TransactionInfo.TransactionId, info.RevocationDate)
			break
		}
		if err := reverseAppleRefund(c, otx, info, asn.Payload.SignedDate, "webhook"); err != nil {
			log.Errorf(c, "[AppleWebhook] refund reversal failed otx=%s: %v", otx, err)
			c.AbortWithStatus(500)
			return
		}

	case appstore.NotificationType_CONSUMPTION_REQUEST:
		// 用户发起退款，Apple 询问消费信息（spec 2026-10-08 §5）。没同意的用户不应答（应答器内跳过）；
		// 发送失败 500 让 Apple 重投——不记 UUID，重投不会被去重门挡掉。
		sandbox := asn.Payload.Data.Environment == appstore.Environment_Sandbox
		if err := respondAppleConsumption(c, &owner, &sub, asn.TransactionInfo.TransactionId, wantBundle, sandbox); err != nil {
			log.Errorf(c, "[AppleWebhook] consumption response failed otx=%s txn=%s: %v", otx, asn.TransactionInfo.TransactionId, err)
			c.AbortWithStatus(500)
			return
		}

	case appstore.NotificationType_REFUND_DECLINED:
		log.Infof(c, "[AppleWebhook] %s otx=%s txn=%s (no action)", nType, otx, asn.TransactionInfo.TransactionId)

	case appstore.NotificationType_EXPIRED, appstore.NotificationType_GRACE_PERIOD_EXPIRED:
		if err := applyAppleExpired(c, &sub); err != nil {
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

// appleWebhookGrant 复核并入账通知附带的交易，把入账哨兵映射成 S2S 重试语义。返回 false 表示已写出
// 错误状态码，调用方必须直接 return（不记 LastEventID，Apple 的重投不会被 UUID 去重门挡掉）。
func appleWebhookGrant(c *gin.Context, sub *Subscription, txnID string) bool {
	otx := sub.ProviderSubscriptionID
	if err := verifyAndGrantTransaction(c, sub.UserID, txnID); errors.Is(err, errAppleTxnRevoked) {
		// 被退款的交易永不入账；视为已处理，不让 Apple 重投。
		log.Infof(c, "[AppleWebhook] txn %s already refunded, not crediting otx=%s", txnID, otx)
	} else if errors.Is(err, errAppleTxnUpgraded) {
		// 同组升级被取代的交易：不是退款，取代它的新交易另行入账；视为已处理。
		log.Infof(c, "[AppleWebhook] txn %s superseded by same-group upgrade, not crediting otx=%s", txnID, otx)
	} else if errors.Is(err, errAppleTxnAwaitingRebind) {
		// 试用链上别的账号的付费交易：等那个账号的 verify 改绑。5xx 让 Apple 重投——改绑之后的重投
		// 命中去重；不记 LastEventID（调用方 return 跳过），重投不会被 UUID 去重门挡掉。
		log.Warnf(c, "[AppleWebhook] txn %s awaits trial-chain rebind, asking Apple to retry otx=%s: %v",
			txnID, otx, err)
		c.AbortWithStatus(503)
		return false
	} else if err != nil {
		log.Errorf(c, "[AppleWebhook] grant failed otx=%s: %v", otx, err)
		c.AbortWithStatus(500)
		return false
	}
	return true
}

// setSubStatus 不覆盖 revoked：Apple 在退款后照常发 EXPIRED，把 revoked 改成 expired 会丢失
// 复活判据（spec 2026-10-08 §3.4）。
func setSubStatus(ctx context.Context, id uint64, status string) error {
	return db.Get().Model(&Subscription{}).Where("id = ? AND status <> ?", id, "revoked").Update("status", status).Error
}

// applyAppleExpired 落地 EXPIRED / GRACE_PERIOD_EXPIRED：expired_at 已等于 Apple expiresDate，
// 自然过期，只标记状态；试用链顺带清 in_trial（不动 expired_at，叠加的赠送照常保留）。
// 两步都幂等，失败由 webhook 500 → Apple 重投整体重来。
func applyAppleExpired(ctx context.Context, sub *Subscription) error {
	if err := setSubStatus(ctx, sub.ID, "expired"); err != nil {
		return err
	}
	return clearInTrialIfTrialChain(db.Get(), sub)
}

func recordSubEventID(ctx context.Context, id uint64, eventID string) error {
	return db.Get().Model(&Subscription{}).Where("id = ?", id).Update("last_event_id", eventID).Error
}

// appleNotificationRefundReversed qtoolkit v1.5.32 尚无此常量（Apple 2023-06 起下发）。
const appleNotificationRefundReversed = "REFUND_REVERSED"

// appleRootCAPEM webhook 的 JWS 信任根（var 仅供端到端测试注入合成 CA）。
var appleRootCAPEM = appstore.AppleRootCAPEM

// verifyAppleJWS validates an Apple JWS signed payload against the embedded
// Apple Root CA G3. Delegates to verifyAppleJWSWithRoot.
func verifyAppleJWS(payload string) error {
	return verifyAppleJWSWithRoot(payload, appleRootCAPEM)
}

// verifyAppleJWSWithRoot performs full two-stage JWS verification:
//  1. Certificate chain: x5c[leaf]→x5c[intermediate]→rootPEM (Apple Root CA G3 in prod).
//  2. Signature: ES256 (ECDSA P-256 + SHA-256) over header.payload using the leaf key.
//     Apple uses RFC 7518 §3.4 raw R||S encoding (64 bytes), not ASN.1 DER.
//
// rootPEM is injectable so tests can use a synthetic CA without real Apple credentials.
func verifyAppleJWSWithRoot(payload string, rootPEM []byte) error {
	parts := strings.Split(payload, ".")
	if len(parts) < 3 {
		return errors.New("invalid JWS: expected header.payload.signature")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode JWS header: %w", err)
	}
	var header struct {
		Alg string   `json:"alg"`
		X5c []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("parse JWS header: %w", err)
	}
	if header.Alg != "ES256" {
		return fmt.Errorf("unexpected JWS algorithm %q; only ES256 accepted", header.Alg)
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

	// Stage 1: certificate chain must root at the injected trust anchor.
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return errors.New("load root CA: PEM parse failed")
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

	// Stage 2: ECDSA signature over header.payload using the leaf's public key.
	// Apple follows RFC 7518 §3.4: signature = R || S (32 bytes each for P-256).
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode JWS signature: %w", err)
	}
	if len(sigBytes) != 64 {
		return fmt.Errorf("ES256 signature must be 64 bytes, got %d", len(sigBytes))
	}
	ecKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("leaf cert: expected ECDSA public key")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])
	if !ecdsa.Verify(ecKey, digest[:], r, s) {
		return errors.New("JWS signature verification failed")
	}
	return nil
}
