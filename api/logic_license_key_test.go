package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyLicenseKeyDiscount_Discount(t *testing.T) {
	key := &LicenseKey{DiscountType: "discount", DiscountValue: 80}
	newAmt, reduced := ApplyLicenseKeyDiscount(key, 1000)
	assert.Equal(t, uint64(800), newAmt)
	assert.Equal(t, uint64(200), reduced)
}

func TestApplyLicenseKeyDiscount_Coupon(t *testing.T) {
	key := &LicenseKey{DiscountType: "coupon", DiscountValue: 200}
	newAmt, reduced := ApplyLicenseKeyDiscount(key, 1000)
	assert.Equal(t, uint64(800), newAmt)
	assert.Equal(t, uint64(200), reduced)
}

func TestApplyLicenseKeyDiscount_CouponExceedsPrice(t *testing.T) {
	key := &LicenseKey{DiscountType: "coupon", DiscountValue: 1500}
	newAmt, reduced := ApplyLicenseKeyDiscount(key, 1000)
	assert.Equal(t, uint64(0), newAmt)
	assert.Equal(t, uint64(1000), reduced)
}

func TestApplyLicenseKeyDiscount_UnknownType(t *testing.T) {
	key := &LicenseKey{DiscountType: "unknown", DiscountValue: 100}
	newAmt, reduced := ApplyLicenseKeyDiscount(key, 1000)
	assert.Equal(t, uint64(1000), newAmt, "unknown type: no discount applied")
	assert.Equal(t, uint64(0), reduced)
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
