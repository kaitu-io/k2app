package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLicenseKey_PlanDays_Default(t *testing.T) {
	key := &LicenseKey{PlanDays: 30}
	assert.Equal(t, 30, key.PlanDays)
}

func TestLicenseKey_PlanDays_Custom(t *testing.T) {
	key := &LicenseKey{PlanDays: 90}
	assert.Equal(t, 90, key.PlanDays)
}

func TestMatchLicenseKey_NeverPaid_NotPaid(t *testing.T) {
	key := &LicenseKey{RecipientMatcher: "never_paid"}
	notDone := false
	user := &User{IsFirstOrderDone: &notDone}
	assert.True(t, MatchLicenseKey(key, user))
}

func TestMatchLicenseKey_NeverPaid_AlreadyPaid(t *testing.T) {
	key := &LicenseKey{RecipientMatcher: "never_paid"}
	done := true
	user := &User{IsFirstOrderDone: &done}
	assert.False(t, MatchLicenseKey(key, user))
}

func TestMatchLicenseKey_NeverPaid_NilField(t *testing.T) {
	key := &LicenseKey{RecipientMatcher: "never_paid"}
	user := &User{IsFirstOrderDone: nil}
	assert.True(t, MatchLicenseKey(key, user), "nil IsFirstOrderDone means never paid")
}

func TestMatchLicenseKey_All(t *testing.T) {
	key := &LicenseKey{RecipientMatcher: "all"}
	done := true
	user := &User{IsFirstOrderDone: &done}
	assert.True(t, MatchLicenseKey(key, user), "'all' matcher always true")
}

func TestLicenseKeyIsExpired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).Unix()
	future := time.Now().Add(24 * time.Hour).Unix()

	expired := &LicenseKey{ExpiresAt: past}
	valid := &LicenseKey{ExpiresAt: future}

	assert.True(t, expired.IsExpired())
	assert.False(t, valid.IsExpired())
}
