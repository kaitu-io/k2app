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

	// "cn-access" preset is [cn-sites, geoip-cn] in v2 schema. HTTPDNS anchor
	// IPs are blended into geoip-cn at bundle build time (k2-rules), not
	// surfaced as a separate set here.
	cn := ExpandPreset("cn-access")
	if cn == nil {
		t.Fatal("ExpandPreset(\"cn-access\") returned nil")
	}
	if !contains(cn, "cn-sites") || !contains(cn, "geoip-cn") {
		t.Errorf("ExpandPreset(\"cn-access\") = %v, want cn-sites and geoip-cn", cn)
	}

	// Country-specific presets follow the "{cc}-access" → [{cc}-sites, geoip-{cc}] shape.
	ir := ExpandPreset("ir-access")
	if ir == nil {
		t.Fatal("ExpandPreset(\"ir-access\") returned nil")
	}
	if !contains(ir, "ir-sites") || !contains(ir, "geoip-ir") {
		t.Errorf("ExpandPreset(\"ir-access\") = %v, want ir-sites and geoip-ir", ir)
	}

	ru := ExpandPreset("ru-access")
	if ru == nil {
		t.Fatal("ExpandPreset(\"ru-access\") returned nil")
	}
	if !contains(ru, "ru-sites") || !contains(ru, "geoip-ru") {
		t.Errorf("ExpandPreset(\"ru-access\") = %v, want ru-sites and geoip-ru", ru)
	}

	mm := ExpandPreset("mm-access")
	if mm == nil {
		t.Fatal("ExpandPreset(\"mm-access\") returned nil")
	}
	if !contains(mm, "mm-sites") || !contains(mm, "geoip-mm") {
		t.Errorf("ExpandPreset(\"mm-access\") = %v, want mm-sites and geoip-mm", mm)
	}

	tm := ExpandPreset("tm-access")
	if tm == nil {
		t.Fatal("ExpandPreset(\"tm-access\") returned nil")
	}
	if !contains(tm, "tm-sites") || !contains(tm, "geoip-tm") {
		t.Errorf("ExpandPreset(\"tm-access\") = %v, want tm-sites and geoip-tm", tm)
	}

	kz := ExpandPreset("kz-access")
	if kz == nil {
		t.Fatal("ExpandPreset(\"kz-access\") returned nil")
	}
	if !contains(kz, "kz-sites") || !contains(kz, "geoip-kz") {
		t.Errorf("ExpandPreset(\"kz-access\") = %v, want kz-sites and geoip-kz", kz)
	}

	uz := ExpandPreset("uz-access")
	if uz == nil {
		t.Fatal("ExpandPreset(\"uz-access\") returned nil")
	}
	if !contains(uz, "uz-sites") || !contains(uz, "geoip-uz") {
		t.Errorf("ExpandPreset(\"uz-access\") = %v, want uz-sites and geoip-uz", uz)
	}

	// "games" is a standalone service bundle, not a "{cc}-access" country
	// preset — no "-access" suffix, and its set names don't follow the
	// {cc}-sites/geoip-{cc} shape.
	games := ExpandPreset("games")
	if games == nil {
		t.Fatal("ExpandPreset(\"games\") returned nil")
	}
	if !contains(games, "games-sites") || !contains(games, "games-ip") {
		t.Errorf("ExpandPreset(\"games\") = %v, want games-sites and games-ip", games)
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

func TestHasCriteria_NewFields(t *testing.T) {
	tests := []struct {
		name string
		mc   MatchConfig
	}{
		{"process_name_prefix", MatchConfig{ProcessNamePrefix: []string{"WeChat"}}},
		{"package_name_prefix", MatchConfig{PackageNamePrefix: []string{"com.tencent."}}},
		{"installer_package",   MatchConfig{InstallerPackage:  []string{"com.xiaomi.market"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.mc.HasCriteria() {
				t.Errorf("HasCriteria should be true for %s", tc.name)
			}
		})
	}
}

func TestMatchConfig_HasCriteria_RegionAndApps(t *testing.T) {
	if !(MatchConfig{Region: "cn"}).HasCriteria() {
		t.Error("MatchConfig{Region: \"cn\"}.HasCriteria() = false, want true")
	}
	if !(MatchConfig{Apps: []string{"Firefox*"}}).HasCriteria() {
		t.Error("MatchConfig{Apps: [...]}.HasCriteria() = false, want true")
	}
	// Empty match still has no criteria.
	if (MatchConfig{}).HasCriteria() {
		t.Error("empty MatchConfig.HasCriteria() = true, want false")
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
