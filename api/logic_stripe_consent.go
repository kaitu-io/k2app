package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// 结账同意（spec 2026-10-07 A 期 §4）。改文案必须同时改 consentTextVersion：同意记录存的是
// 版本号，取证时靠它对回当时的原文（git 历史）。

const consentTextVersion = "2026-10-v1"

// stripeConsentMessage Checkout 上"同意条款"勾选框旁的文案（Stripe 支持 markdown 链接，≤1200 字符）。
// 范围与条款 7.2 一致：首付或年付续费后 14 天内撤回，按已用天数折算。
func stripeConsentMessage() string {
	return "I agree to the [Terms of Service](" + BrandOverleap.Config().BaseURL + "/terms). " +
		"My subscription starts now and renews automatically at the regular price shown " +
		"(or a new price you tell me about in advance) until I cancel. " +
		"I ask for the service to begin immediately. I understand that if I withdraw within 14 days " +
		"of my first payment, or of an annual renewal, my refund will be reduced for the days already used."
}

// recordStripeCheckoutConsent 落地 checkout.session.completed：用户勾了条款就写一条
// SubscriptionConsent（按 session id 幂等）。用户找不到只告警不报错——同意记录缺失不阻断入账，
// 撤回时按"无同意"全额退（quoteStripeWithdrawal）。
func recordStripeCheckoutConsent(ctx context.Context, raw []byte, eventCreated int64) error {
	var s stripe.CheckoutSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse checkout session: %w", err)
	}
	if s.Consent == nil || s.Consent.TermsOfService != stripe.CheckoutSessionConsentTermsOfServiceAccepted {
		log.Warnf(ctx, "[StripeConsent] session %s completed without terms acceptance", s.ID)
		return nil
	}
	if s.ClientReferenceID == "" {
		// GORM 会忽略零值条件 → 空 uuid 会匹配任意用户。我们的 Checkout 恒带 client_reference_id；
		// 空的只可能是同一 Stripe 账号上别处建的 Checkout。
		alertStripeRevoke(ctx, "[STRIPE-CONSENT]", "session %s has no client_reference_id — consent not recorded", s.ID)
		return nil
	}
	var u User
	if err := getDB().Select("id").Where("uuid = ?", s.ClientReferenceID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			alertStripeRevoke(ctx, "[STRIPE-CONSENT]", "session %s client_reference_id %q matches no user — consent not recorded", s.ID, s.ClientReferenceID)
			return nil
		}
		return err
	}
	row := &SubscriptionConsent{
		UserID:            u.ID,
		CheckoutSessionID: s.ID,
		TextVersion:       consentTextVersion,
		AcceptedAt:        eventCreated,
	}
	if s.Subscription != nil {
		row.ProviderSubscriptionID = s.Subscription.ID
	}
	if s.CustomerDetails != nil && s.CustomerDetails.Address != nil {
		row.Country = s.CustomerDetails.Address.Country
	}
	if err := getDB().Create(row).Error; err != nil {
		if util.DbIsDuplicatedErr(err) {
			return nil // 重投
		}
		return err
	}
	log.Infof(ctx, "[StripeConsent] user %d accepted %s (session %s, sub %s, country %q)",
		u.ID, consentTextVersion, s.ID, row.ProviderSubscriptionID, row.Country)
	return nil
}
