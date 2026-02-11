package daemon

import "testing"

func TestApplyResourceLimits(t *testing.T) {
	// Should not panic.
	ApplyResourceLimits()
}
