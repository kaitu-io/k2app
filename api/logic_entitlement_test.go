package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyGiftCredit(t *testing.T) {
	const now int64 = 1_000_000
	const day int64 = 86400
	// active ledger: stack on current expiry
	assert.Equal(t, now+10*day+7*day, applyGiftCredit(now+10*day, 7*day, now))
	// expired ledger: start from now
	assert.Equal(t, now+7*day, applyGiftCredit(now-5*day, 7*day, now))
	// exactly now counts as expired → from now
	assert.Equal(t, now+7*day, applyGiftCredit(now, 7*day, now))
}

func TestApplyRenewalCredit(t *testing.T) {
	const day int64 = 86400
	base := int64(1_700_000_000)
	priorEnd := base + 365*day     // apple already covered to priorEnd
	newEnd := priorEnd + 365*day   // renewal extends one year
	// no gift on top → ledger rides exactly with apple
	assert.Equal(t, newEnd, applyRenewalCredit(priorEnd, priorEnd, newEnd))
	// 7-day gift sat on top of apple coverage → renewal preserves the gift (no absorption)
	withGift := priorEnd + 7*day
	assert.Equal(t, withGift+365*day, applyRenewalCredit(withGift, priorEnd, newEnd))
	// non-advancing (replay / out-of-order) credits nothing
	assert.Equal(t, withGift, applyRenewalCredit(withGift, priorEnd, priorEnd))
	assert.Equal(t, withGift, applyRenewalCredit(withGift, priorEnd, priorEnd-day))
	// ledger behind apple's prior coverage → realign up to apple then add delta
	assert.Equal(t, newEnd, applyRenewalCredit(priorEnd-100, priorEnd, newEnd))
}

func TestApplyClawback(t *testing.T) {
	const day int64 = 86400
	now := int64(1_700_000_000)
	// remove the refunded span
	assert.Equal(t, now+3*day, applyClawback(now+10*day, 7*day, now))
	// never below now
	assert.Equal(t, now, applyClawback(now+2*day, 10*day, now))
}

func TestCoverThrough(t *testing.T) {
	const day int64 = 86400
	now := int64(1_700_000_000)
	assert.Equal(t, now+5*day, coverThrough(now+5*day, now+3*day)) // already covers further
	assert.Equal(t, now+5*day, coverThrough(now+2*day, now+5*day)) // extend to grace end
}
