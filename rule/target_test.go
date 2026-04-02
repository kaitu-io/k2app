package rule

import (
	"testing"
)

func TestTargetString(t *testing.T) {
	tests := []struct {
		target Target
		want   string
	}{
		{TargetDirect, "direct"},
		{TargetProxy, "proxy"},
		{TargetReject, "reject"},
		{Target(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.target.String(); got != tt.want {
			t.Errorf("Target(%d).String() = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestParseVia(t *testing.T) {
	tests := []struct {
		via  string
		want Target
	}{
		{"direct", TargetDirect},
		{"reject", TargetReject},
		{"k2v5://example.com:443", TargetProxy},
		{"k2v5://10.0.0.1:8443?insecure=1", TargetProxy},
		{"", TargetDirect},
		{"something-unknown", TargetDirect},
	}
	for _, tt := range tests {
		if got := ParseVia(tt.via); got != tt.want {
			t.Errorf("ParseVia(%q) = %v, want %v", tt.via, got, tt.want)
		}
	}
}

func TestExpandPreset(t *testing.T) {
	// "overseas" preset should include well-known names.
	overseas := ExpandPreset("overseas")
	if overseas == nil {
		t.Fatal("ExpandPreset(\"overseas\") returned nil")
	}
	wantInOverseas := []string{"google", "facebook", "telegram", "openai", "github"}
	for _, name := range wantInOverseas {
		if !contains(overseas, name) {
			t.Errorf("ExpandPreset(\"overseas\") missing %q", name)
		}
	}

	// "cn-access" preset should work.
	cn := ExpandPreset("cn-access")
	if cn == nil {
		t.Fatal("ExpandPreset(\"cn-access\") returned nil")
	}
	if !contains(cn, "bilibili") {
		t.Error("ExpandPreset(\"cn-access\") missing \"bilibili\"")
	}

	// Unknown preset returns nil.
	if got := ExpandPreset("nonexistent"); got != nil {
		t.Errorf("ExpandPreset(\"nonexistent\") = %v, want nil", got)
	}

	// Returned slice should be a copy (not mutate internal state).
	first := ExpandPreset("overseas")
	first[0] = "MUTATED"
	second := ExpandPreset("overseas")
	if second[0] == "MUTATED" {
		t.Error("ExpandPreset returned a reference to internal slice, not a copy")
	}
}

func TestExpandMatch(t *testing.T) {
	// All=true returns nil.
	got := ExpandMatch(MatchConfig{All: true})
	if got != nil {
		t.Errorf("ExpandMatch(All=true) = %v, want nil", got)
	}

	// Preset only.
	got = ExpandMatch(MatchConfig{Preset: "cn-access"})
	if !contains(got, "bilibili") {
		t.Errorf("ExpandMatch(preset=cn-access) missing \"bilibili\": %v", got)
	}

	// Preset + extra names.
	got = ExpandMatch(MatchConfig{
		Preset: "cn-access",
		Names:  []string{"custom-site"},
	})
	if !contains(got, "bilibili") || !contains(got, "custom-site") {
		t.Errorf("ExpandMatch(preset+names) = %v, want bilibili and custom-site", got)
	}

	// Preset + exclude.
	got = ExpandMatch(MatchConfig{
		Preset:  "overseas",
		Exclude: []string{"google", "facebook"},
	})
	if contains(got, "google") || contains(got, "facebook") {
		t.Errorf("ExpandMatch with excludes still contains excluded names: %v", got)
	}
	if !contains(got, "telegram") {
		t.Errorf("ExpandMatch with excludes missing non-excluded name \"telegram\": %v", got)
	}

	// Names only (no preset).
	got = ExpandMatch(MatchConfig{Names: []string{"a", "b"}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("ExpandMatch(names only) = %v, want [a b]", got)
	}

	// Empty config returns empty slice.
	got = ExpandMatch(MatchConfig{})
	if got == nil || len(got) != 0 {
		t.Errorf("ExpandMatch(empty) = %v, want empty non-nil slice", got)
	}
}

func TestHasCriteria(t *testing.T) {
	tests := []struct {
		name string
		m    MatchConfig
		want bool
	}{
		{"empty", MatchConfig{}, false},
		{"preset", MatchConfig{Preset: "overseas"}, true},
		{"names", MatchConfig{Names: []string{"x"}}, true},
		{"exclude only", MatchConfig{Exclude: []string{"x"}}, true},
		{"all", MatchConfig{All: true}, true},
	}
	for _, tt := range tests {
		if got := tt.m.HasCriteria(); got != tt.want {
			t.Errorf("HasCriteria(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
