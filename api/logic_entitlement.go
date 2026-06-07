package center

// logic_entitlement.go — the ONLY place entitlement deltas are computed.
// expired_at is mutated solely through these results (INV2/INV8 enforced by callers).

// applyGiftCredit adds `seconds` of entitlement: from now if the ledger is already
// expired, else stacked on the current expiry. Used for one-time/gift credits and
// the FIRST transaction of a new subscription (no prior recurring coverage).
func applyGiftCredit(currentExpiredAt, seconds, now int64) int64 {
	base := currentExpiredAt
	if base < now {
		base = now
	}
	return base + seconds
}

// applyRenewalCredit extends entitlement by a recurring renewal. priorPeriodEnd is
// the coverage this subscription already provided (its stored current_period_end);
// newPeriodEnd is the transaction's expiresDate. Credits only the forward delta,
// stacked on top of whatever the ledger holds, so gifts are never absorbed (INV3).
// Monotonic & safe: a non-advancing transaction credits 0.
func applyRenewalCredit(currentExpiredAt, priorPeriodEnd, newPeriodEnd int64) int64 {
	delta := newPeriodEnd - priorPeriodEnd
	if delta <= 0 {
		return currentExpiredAt
	}
	base := currentExpiredAt
	if base < priorPeriodEnd {
		base = priorPeriodEnd
	}
	return base + delta
}

// applyClawback removes `seconds` on refund/revoke, never pushing below now (INV2).
func applyClawback(currentExpiredAt, seconds, now int64) int64 {
	v := currentExpiredAt - seconds
	if v < now {
		v = now
	}
	return v
}

// coverThrough ensures the ledger covers at least throughTs (grace window); never
// shortens. Used by Phase 2 grace handling.
func coverThrough(currentExpiredAt, throughTs int64) int64 {
	if throughTs > currentExpiredAt {
		return throughTs
	}
	return currentExpiredAt
}
