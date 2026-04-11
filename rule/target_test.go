package rule

import (
	"testing"
)

func TestTargetString(t *testing.T) {
	tests := []struct {
		target Target
		want   string
	}{
		{Target(0), "outbound(0)"},
		{Target(1), "outbound(1)"},
		{Target(2), "outbound(2)"},
		{Target(3), "outbound(3)"},
		{Target(99), "outbound(99)"},
	}
	for _, tt := range tests {
		if got := tt.target.String(); got != tt.want {
			t.Errorf("Target(%d).String() = %q, want %q", tt.target, got, tt.want)
		}
	}
}


func TestExpandPreset(t *testing.T) {
	// "overseas" preset is a single consolidated set in v2 schema.
	overseas := ExpandPreset("overseas")
	if overseas == nil {
		t.Fatal("ExpandPreset(\"overseas\") returned nil")
	}
	if len(overseas) != 1 || overseas[0] != "overseas" {
		t.Errorf("ExpandPreset(\"overseas\") = %v, want [overseas]", overseas)
	}

	// "cn-access" preset is [cn-sites, geoip-cn] in v2 schema.
	cn := ExpandPreset("cn-access")
	if cn == nil {
		t.Fatal("ExpandPreset(\"cn-access\") returned nil")
	}
	if !contains(cn, "cn-sites") || !contains(cn, "geoip-cn") {
		t.Errorf("ExpandPreset(\"cn-access\") = %v, want cn-sites and geoip-cn", cn)
	}

	// Unknown preset returns nil.
	if got := ExpandPreset("nonexistent"); got != nil {
		t.Errorf("ExpandPreset(\"nonexistent\") = %v, want nil", got)
	}

	// Returned slice should be a copy (not mutate internal state).
	first := ExpandPreset("cn-access")
	first[0] = "MUTATED"
	second := ExpandPreset("cn-access")
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
	if !contains(got, "cn-sites") || !contains(got, "geoip-cn") {
		t.Errorf("ExpandMatch(preset=cn-access) = %v, want cn-sites and geoip-cn", got)
	}

	// Preset + extra names.
	got = ExpandMatch(MatchConfig{
		Preset: "cn-access",
		Names:  []string{"custom-site"},
	})
	if !contains(got, "cn-sites") || !contains(got, "custom-site") {
		t.Errorf("ExpandMatch(preset+names) = %v, want cn-sites and custom-site", got)
	}

	// Preset + exclude.
	got = ExpandMatch(MatchConfig{
		Preset:  "cn-access",
		Exclude: []string{"cn-sites"},
	})
	if contains(got, "cn-sites") {
		t.Errorf("ExpandMatch with excludes still contains excluded name: %v", got)
	}
	if !contains(got, "geoip-cn") {
		t.Errorf("ExpandMatch with excludes missing non-excluded name \"geoip-cn\": %v", got)
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
		{"domain_suffix", MatchConfig{DomainSuffix: []string{"google.com"}}, true},
		{"ip_cidr", MatchConfig{IPCIDR: []string{"8.8.8.0/24"}}, true},
		{"process_name", MatchConfig{ProcessName: []string{"Chrome"}}, true},
		{"package_name", MatchConfig{PackageName: []string{"com.android.chrome"}}, true},
		{"network", MatchConfig{Network: "tcp"}, true},
		{"ip_is_private", MatchConfig{IPIsPrivate: true}, true},
	}
	for _, tt := range tests {
		if got := tt.m.HasCriteria(); got != tt.want {
			t.Errorf("HasCriteria(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHasHostCriteria(t *testing.T) {
	tests := []struct {
		name string
		m    MatchConfig
		want bool
	}{
		{"empty", MatchConfig{}, false},
		{"preset", MatchConfig{Preset: "overseas"}, true},
		{"domain_suffix", MatchConfig{DomainSuffix: []string{"google.com"}}, true},
		{"ip_cidr", MatchConfig{IPCIDR: []string{"8.8.8.0/24"}}, true},
		{"ip_is_private", MatchConfig{IPIsPrivate: true}, true},
		{"process_name only", MatchConfig{ProcessName: []string{"ssh"}}, false},
		{"network only", MatchConfig{Network: "udp"}, false},
		{"package_name only", MatchConfig{PackageName: []string{"com.x"}}, false},
	}
	for _, tt := range tests {
		if got := tt.m.HasHostCriteria(); got != tt.want {
			t.Errorf("HasHostCriteria(%s) = %v, want %v", tt.name, got, tt.want)
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
