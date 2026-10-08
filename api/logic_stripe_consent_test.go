package center

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func checkoutCompletedPayload(evtID, sessionID, userUUID, subID, consent, country string) []byte {
	consentField := "null"
	if consent != "" {
		consentField = fmt.Sprintf(`{"terms_of_service": %q, "promotions": null}`, consent)
	}
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "checkout.session.completed", "livemode": false, "created": 1790000000,
		"data": {"object": {"id": %q, "object": "checkout.session", "client_reference_id": %q,
			"subscription": %q, "consent": %s,
			"customer_details": {"address": {"country": %q}}}}
	}`, evtID, sessionID, userUUID, subID, consentField, country))
}

// #31 checkout.session.completed → SubscriptionConsent；重投不重复；国家可为空；未勾选不写
func TestStripeCheckoutConsent(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", stripeTestWebhookSecret)
	installStripeFakes(t)
	r := stripeWebhookRouter()

	u := createStripeTestUser(t, BrandOverleap)
	t.Cleanup(func() { db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&SubscriptionConsent{}) })
	post := func(payload []byte, evtID string) int {
		cleanupStripeEvents(t, evtID)
		return postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code
	}

	sess, sub := "cs_"+stripeUniq(), "sub_"+stripeUniq()
	e1, e2 := "evt_"+stripeUniq(), "evt_"+stripeUniq()
	require.Equal(t, 200, post(checkoutCompletedPayload(e1, sess, u.UUID, sub, "accepted", "GB"), e1))
	require.Equal(t, 200, post(checkoutCompletedPayload(e2, sess, u.UUID, sub, "accepted", "GB"), e2)) // 不同 event 重投同一 session

	var rows []SubscriptionConsent
	require.NoError(t, db.Get().Where("user_id = ?", u.ID).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, sub, rows[0].ProviderSubscriptionID)
	assert.Equal(t, consentTextVersion, rows[0].TextVersion)
	assert.Equal(t, "GB", rows[0].Country)
	assert.Equal(t, int64(1790000000), rows[0].AcceptedAt)

	// client_reference_id 为空：不能匹配到任意用户
	eEmpty, emptySess := "evt_"+stripeUniq(), "cs_"+stripeUniq()
	t.Cleanup(func() { db.Get().Unscoped().Where("checkout_session_id = ?", emptySess).Delete(&SubscriptionConsent{}) })
	require.Equal(t, 200, post(checkoutCompletedPayload(eEmpty, emptySess, "", "sub_"+stripeUniq(), "accepted", "GB"), eEmpty))
	var stray int64
	db.Get().Model(&SubscriptionConsent{}).Where("checkout_session_id = ?", emptySess).Count(&stray)
	assert.Zero(t, stray, "empty client_reference_id must not attach consent to any user")

	e3 := "evt_" + stripeUniq()
	require.Equal(t, 200, post(checkoutCompletedPayload(e3, "cs_"+stripeUniq(), u.UUID, "sub_"+stripeUniq(), "accepted", ""), e3))
	e4 := "evt_" + stripeUniq()
	require.Equal(t, 200, post(checkoutCompletedPayload(e4, "cs_"+stripeUniq(), u.UUID, "sub_"+stripeUniq(), "", "US"), e4))
	require.NoError(t, db.Get().Where("user_id = ?", u.ID).Find(&rows).Error)
	assert.Len(t, rows, 2, "empty country recorded; no-acceptance not recorded")
}
