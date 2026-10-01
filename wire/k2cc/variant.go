package k2cc

import (
	"fmt"
	"strconv"
	"strings"
)

// A/B arms. Every arm is GFW mode (β, ε unchanged); they differ only in loss
// attribution, BW-phase alignment and α. Spec:
// docs/superpowers/specs/2026-10-01-k2cc-loss-attribution-design.md.
const (
	// VariantControl is today's behaviour, bit for bit.
	VariantControl = "control"
	// VariantFixA4 / A3 / A15: resolved-denominator loss + aligned BW phases.
	// α sets the real-loss boundary of the PCC gradient, 1/(1+α): 20 %, 25 %,
	// 40 % (the last is what control effectively tolerates today, since it
	// sees about half of the loss).
	VariantFixA4  = "fix-a4"
	VariantFixA3  = "fix-a3"
	VariantFixA15 = "fix-a15"
	// Lab-only decomposition arms — never deploy: each applies one half of
	// the fix so the benchmark matrix can attribute an effect to its cause.
	VariantResolvedOnly = "lab-resolved-only" // resolved loss, legacy (off-by-one) phases
	VariantAlignOnly    = "lab-align-only"    // legacy loss, aligned phases
)

// variantConfig returns the config for an arm; ok=false for an unknown name.
func variantConfig(name string) (*K2CCConfig, bool) {
	c := &K2CCConfig{Alpha: gfwAlpha, Beta: defaultBeta, Epsilon: defaultEpsilon, Variant: name}
	switch name {
	case VariantControl:
	case VariantFixA4:
		c.LossAttribution, c.AlignBWPhases = LossAttrResolved, true
	case VariantFixA3:
		c.LossAttribution, c.AlignBWPhases, c.Alpha = LossAttrResolved, true, 3.0
	case VariantFixA15:
		c.LossAttribution, c.AlignBWPhases, c.Alpha = LossAttrResolved, true, 1.5
	case VariantResolvedOnly:
		c.LossAttribution = LossAttrResolved
	case VariantAlignOnly:
		c.AlignBWPhases = true
	default:
		return nil, false
	}
	return c, true
}

// NewCCSenderVariant creates a GFW-mode sender for an A/B arm. An unknown
// name yields the control arm.
func NewCCSenderVariant(name string, maxRate int64) *K2CCSender {
	c, ok := variantConfig(name)
	if !ok {
		c, _ = variantConfig(VariantControl)
	}
	return NewK2CCSenderWithConfig(maxRate, c)
}

// VariantPicker assigns arms by weight.
type VariantPicker struct {
	names []string
	cum   []int // cumulative weights
}

// ParseVariants parses "control:50,fix-a3:25,fix-a15:25". Weights are
// positive integers; names must be known arms. "" is 100 % control.
func ParseVariants(spec string) (*VariantPicker, error) {
	p := &VariantPicker{}
	total := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, w, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("k2cc variants: %q is not name:weight", part)
		}
		name = strings.TrimSpace(name)
		if _, known := variantConfig(name); !known {
			return nil, fmt.Errorf("k2cc variants: unknown arm %q", name)
		}
		n, err := strconv.Atoi(strings.TrimSpace(w))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("k2cc variants: weight of %q must be a positive integer", name)
		}
		total += n
		p.names = append(p.names, name)
		p.cum = append(p.cum, total)
	}
	if total == 0 {
		p.names, p.cum = []string{VariantControl}, []int{1}
	}
	return p, nil
}

// Pick maps u in [0,1) to an arm.
func (p *VariantPicker) Pick(u float64) string {
	total := p.cum[len(p.cum)-1]
	x := int(u * float64(total))
	for i, c := range p.cum {
		if x < c {
			return p.names[i]
		}
	}
	return p.names[len(p.names)-1]
}

// Weights returns "name:weight" pairs for logging.
func (p *VariantPicker) String() string {
	var b strings.Builder
	prev := 0
	for i, n := range p.names {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%d", n, p.cum[i]-prev)
		prev = p.cum[i]
	}
	return b.String()
}
