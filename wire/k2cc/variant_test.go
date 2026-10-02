package k2cc

import (
	"math"
	"testing"
)

func TestVariantConfig_ControlIsTodaysGFWMode(t *testing.T) {
	c, ok := variantConfig(VariantControl)
	if !ok {
		t.Fatal("control arm unknown")
	}
	want := NewCCSenderGFWMode(DefaultMaxRate).config
	if c.Alpha != want.Alpha || c.Beta != want.Beta || c.Epsilon != want.Epsilon ||
		c.LossAttribution != LossAttrLegacy || c.AlignBWPhases {
		t.Errorf("control = %+v, want today's GFW mode %+v with legacy attribution and phases", c, want)
	}
}

func TestVariantConfig_FixArms(t *testing.T) {
	for name, alpha := range map[string]float64{VariantFixA4: 4, VariantFixA3: 3, VariantFixA15: 1.5} {
		c, ok := variantConfig(name)
		if !ok || c.Alpha != alpha || c.LossAttribution != LossAttrResolved || !c.AlignBWPhases || c.Variant != name {
			t.Errorf("%s = %+v (ok=%v), want resolved+aligned α=%v", name, c, ok, alpha)
		}
	}
	if s := NewCCSenderVariant("no-such-arm", DefaultMaxRate); s.config.LossAttribution != LossAttrLegacy || s.config.AlignBWPhases {
		t.Errorf("unknown arm must fall back to control, got %+v", s.config)
	}
}

func TestParseVariants(t *testing.T) {
	p, err := ParseVariants("")
	if err != nil || p.Pick(0) != VariantControl || p.Pick(0.999) != VariantControl {
		t.Fatalf("empty spec must be 100%% control (err=%v)", err)
	}
	p, err = ParseVariants(" control:50, fix-a3:25,fix-a15:25 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.String(); got != "control:50,fix-a3:25,fix-a15:25" {
		t.Errorf("String() = %q", got)
	}
	counts := map[string]int{}
	const n = 10000
	for i := 0; i < n; i++ {
		counts[p.Pick(float64(i)/n)]++
	}
	for name, want := range map[string]float64{VariantControl: 0.5, VariantFixA3: 0.25, VariantFixA15: 0.25} {
		if got := float64(counts[name]) / n; math.Abs(got-want) > 0.001 {
			t.Errorf("%s share = %.4f, want %.2f", name, got, want)
		}
	}
	for _, bad := range []string{"control", "nope:10", "control:0", "control:-3", "control:x"} {
		if _, err := ParseVariants(bad); err == nil {
			t.Errorf("ParseVariants(%q) accepted", bad)
		}
	}
}
