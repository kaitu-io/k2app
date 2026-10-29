package rule

import (
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

// buildSuffixSet compiles an inline domain-suffix matcher (mirrors what
// engine buildRouteEntries produces for {domain_suffix: [...]}).
func buildSuffixSet(t *testing.T, suffixes []string) krs.Matcher {
	t.Helper()
	set, err := krs.BuildSet(krs.NamedSet{DomainSuffixes: suffixes})
	if err != nil {
		t.Fatalf("BuildSet: %v", err)
	}
	return set
}

func TestMatchDNSMeta_PureProcessRouteMatches(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), ProcessNames: []string{"douyin.exe"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if got, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: "douyin.exe"}); !ok || got != Target(0) {
		t.Errorf("douyin.exe = (%v, %v), want (direct, true)", got, ok)
	}
	if _, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: "chrome.exe"}); ok {
		t.Error("chrome.exe matched, want no match")
	}
	// Empty identity → never matches (fast path, no route scan).
	if _, ok := eng.MatchDNSMeta(ConnMeta{}); ok {
		t.Error("empty meta matched, want no match")
	}
}

// Routes with host criteria never participate: the domain half of the
// decision already had its say via Match(domain).
func TestMatchDNSMeta_HostCriteriaRouteSkipped(t *testing.T) {
	routes := []RouteEntry{
		{
			Target:       Target(0),
			ProcessNames: []string{"douyin.exe"},
			Sets:         []krs.Matcher{buildSuffixSet(t, []string{"example.com"})},
		},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if _, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: "douyin.exe"}); ok {
		t.Error("route with host criteria matched at DNS-meta time, want skip")
	}
}

// A combined route like {apps, port: 443} describes the app's DATA
// connections. The DNS query's own port must not stand in for them —
// matchMeta's AND semantics fail closed on Port==0.
func TestMatchDNSMeta_CombinedPortRouteFailsClosed(t *testing.T) {
	routes := []RouteEntry{
		{
			Target:       Target(0),
			ProcessNames: []string{"douyin.exe"},
			PortRanges:   []PortRange{{Lo: 443, Hi: 443}},
		},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if _, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: "douyin.exe"}); ok {
		t.Error("{apps, port} route matched the DNS query itself, want fail-closed")
	}
}

// Non-process meta routes ({network}, {port}) never participate — they don't
// carry process criteria, and the query's own transport must not match them.
func TestMatchDNSMeta_NonProcessMetaRouteSkipped(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), NetworkMatch: "udp"},
		{Target: Target(3), ProcessNames: []string{"weixin.exe"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	// The udp route must be invisible even though a DNS query IS udp.
	if got, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: "weixin.exe"}); !ok || got != Target(3) {
		t.Errorf("weixin.exe = (%v, %v), want (3, true) — first PROCESS route, not the network route", got, ok)
	}
}

// Android identity dimensions participate the same way.
func TestMatchDNSMeta_PackageName(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), PackageNames: []string{"com.ss.android.ugc.aweme"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if got, ok := eng.MatchDNSMeta(ConnMeta{PackageName: "com.ss.android.ugc.aweme"}); !ok || got != Target(0) {
		t.Errorf("aweme = (%v, %v), want (direct, true)", got, ok)
	}
}

// The owner-UID path fills PackageNames (shared-UID full set) and may leave
// PackageName mirroring only the primary — the DNS meta gate and the matcher
// must both honor the list.
func TestMatchDNSMeta_SharedUIDPackageNames(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), PackageNames: []string{"com.android.vending"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	meta := ConnMeta{PackageNames: []string{"com.google.android.gms", "com.android.vending"}}
	if got, ok := eng.MatchDNSMeta(meta); !ok || got != Target(0) {
		t.Errorf("shared-UID = (%v, %v), want (direct, true)", got, ok)
	}
}
