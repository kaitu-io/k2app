package center

import "testing"

func TestFunnelEventAllowed(t *testing.T) {
	cases := []struct {
		name, surface string
		want          bool
	}{
		{"page_view", "web", true},
		{"page_view", "app", false},
		{"checkout_start", "app", true},
		{"checkout_start", "web", true},
		{"connect_ok", "app", true},
		{"connect_ok", "web", false},
		{"purchase", "web", false},
		{"purchase", "app", false},
		{"nope", "web", false},
	}
	for _, c := range cases {
		if got := funnelEventAllowed(c.name, c.surface); got != c.want {
			t.Errorf("funnelEventAllowed(%q,%q)=%v want %v", c.name, c.surface, got, c.want)
		}
	}
}

func TestFunnelEventKind(t *testing.T) {
	if funnelEventKind("page_view") != FunnelKindView || funnelEventKind("plan_select") != FunnelKindAction ||
		funnelEventKind("connect_ok") != FunnelKindAction ||
		funnelEventKind("purchase") != FunnelKindFact || funnelEventKind("nope") != "" {
		t.Fatal("funnelEventKind mismatch")
	}
}

func TestFunnelEventRegistry_NoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range funnelEventRegistry {
		if seen[d.Name] {
			t.Errorf("duplicate event %q", d.Name)
		}
		seen[d.Name] = true
		if d.Kind == FunnelKindFact {
			if len(d.Surfaces) != 0 {
				t.Errorf("fact %q must have no surfaces", d.Name)
			}
			continue
		}
		if len(d.Surfaces) == 0 {
			t.Errorf("%q has no surfaces", d.Name)
		}
		for _, s := range d.Surfaces {
			if s != FunnelSurfaceWeb && s != FunnelSurfaceApp {
				t.Errorf("%q bad surface %q", d.Name, s)
			}
		}
	}
}
