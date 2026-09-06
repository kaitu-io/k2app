package center

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// ticketNotifyFixture seeds a user of the given brand with a decryptable email
// identity, one open ticket stamped with that brand, and one un-notified admin
// reply. Everything is cleaned up via t.Cleanup.
func ticketNotifyFixture(t *testing.T, brand Brand) (FeedbackTicket, TicketReply) {
	t.Helper()
	nano := time.Now().UnixNano()
	uniq := fmt.Sprintf("%s-%d", brand, nano)

	user := User{UUID: "usr-tnotify-" + uniq, Brand: string(brand)}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	enc, err := secretEncryptString(context.Background(), "tnotify-"+uniq+"@example.com")
	require.NoError(t, err)
	li := LoginIdentify{UserID: user.ID, Type: "email", IndexID: "tnotify-" + uniq, EncryptedValue: enc, Brand: user.Brand}
	require.NoError(t, db.Get().Create(&li).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&li) })

	uid := user.ID
	now := time.Now()
	ticket := FeedbackTicket{
		FeedbackID:  fmt.Sprintf("fb-tn-%d", nano), // feedback_id is varchar(36)
		UserID:      &uid,
		Brand:       string(brand),
		Content:     "ticket body",
		Status:      "open",
		Meta:        "{}",
		LastReplyAt: &now,
	}
	require.NoError(t, db.Get().Create(&ticket).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&ticket) })

	reply := TicketReply{TicketID: ticket.ID, SenderType: "admin", SenderName: "Support", Content: "hello from support"}
	require.NoError(t, db.Get().Create(&reply).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&reply) })

	return ticket, reply
}

// TestHandleTicketNotify_PerBrand drives the real worker against dev MySQL in
// mail dev mode (logs instead of sending): for each brand the notification path
// must complete without error and stamp notified_at on the admin reply. The
// brand → copy/sender selection itself is pinned by the pure unit tests
// (TestBrandedDeviceKickAndTicketNotify, TestTicketBrandFallsBackToKaitu).
func TestHandleTicketNotify_PerBrand(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	origDev := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true)
	t.Cleanup(func() { viper.Set("mail.dev_mode", origDev) })

	for _, brand := range []Brand{BrandOverleap, BrandKaitu} {
		t.Run(string(brand), func(t *testing.T) {
			ticket, reply := ticketNotifyFixture(t, brand)

			payload, err := json.Marshal(TicketNotifyPayload{TicketID: ticket.ID})
			require.NoError(t, err)
			require.NoError(t, handleTicketNotify(context.Background(), payload))

			var got TicketReply
			require.NoError(t, db.Get().First(&got, reply.ID).Error)
			assert.NotNil(t, got.NotifiedAt, "admin reply must be marked notified after the email path completes")

			var stored FeedbackTicket
			require.NoError(t, db.Get().First(&stored, ticket.ID).Error)
			assert.Equal(t, string(brand), stored.Brand, "brand column must round-trip through the DB")
		})
	}
}
