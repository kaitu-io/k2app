package wire

import (
	"testing"

	"github.com/kaitu-io/k2/wire/k2cc"
)

func TestLoadCCVariants(t *testing.T) {
	// Unset → every connection is control: shipping the binary is a no-op.
	p := loadCCVariants("")
	for _, u := range []float64{0, 0.5, 0.999} {
		if got := p.Pick(u); got != k2cc.VariantControl {
			t.Errorf("unset: Pick(%v) = %q, want control", u, got)
		}
	}
	// Invalid → control for everyone, never a half-applied split.
	p = loadCCVariants("control:50,typo-arm:50")
	if got := p.Pick(0.9); got != k2cc.VariantControl {
		t.Errorf("invalid spec: Pick = %q, want control", got)
	}
	p = loadCCVariants("control:50,fix-a3:50")
	if p.Pick(0.1) != k2cc.VariantControl || p.Pick(0.9) != k2cc.VariantFixA3 {
		t.Errorf("valid spec not honoured: %s", p)
	}
}
