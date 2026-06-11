package engine

// Control-plane direct route: a {via:'direct', match:{domain_suffix:[host]}} route
// prepended to an otherwise-global config must keep that host's traffic OFF the
// tunnel in TUN mode, across every routing preset — including global/allProxy
// where SNI sniffing is disabled.
//
// Why this matters: the webapp pins the Kaitu API host to direct so the app's own
// requests never egress the tunnel (a tunneled API call carries the exit node's
// IP and poisons Center's IP-based geo detection). The route is honored via the
// DNS-learning path:
//   1. DNS-time:  Match(host)            -> direct          (route fires)
//   2. learn:     SetTmpRule(ip, direct)                    (DNS middleware step 10)
//   3. conn-time: Match(ip)              -> direct          (tmp[ip] hit)
//
// Regression guard: SetTmpRule previously skipped ALL pins when allProxy was set,
// which silently dropped step 2 in global/bypass mode (both emit match:{all:true}).
// The fix keeps pinning when exception routes exist (len(e.routes) > 0).

import (
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

const (
	cpTDirect = rule.Target(0)
	cpTProxy  = rule.Target(2)

	cpAPIHost     = "k2.52j.me"
	cpAPISubHost  = "api.k2.52j.me"
	cpServerURL   = "k2v5://udid:tok@host:443"
	cpResolvedIP  = "13.224.55.99"  // pretend CDN A-record for the API host
	cpUnrelatedIP = "93.184.216.34" // some other destination
)

func cpDirectRoute(hosts ...string) config.RouteConfig {
	return config.RouteConfig{Via: "direct", Match: rule.MatchConfig{DomainSuffix: hosts}}
}

// learn replays the DNS middleware: match the domain, then pin each resolved IP
// to the matched target (core/dns/middleware.go processQuery steps 7 + 10).
func learn(re *rule.Engine, domain string, ips ...string) rule.Target {
	action := re.Match(domain)
	for _, ip := range ips {
		re.SetTmpRule(ip, action)
	}
	return action
}

// assertControlPlaneDirect runs the full DNS-learn → connect path and checks the
// API host (and a subdomain) routes direct while unrelated traffic does not.
func assertControlPlaneDirect(t *testing.T, client *config.ClientConfig) {
	t.Helper()
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()

	if got := learn(re, cpAPIHost, cpResolvedIP); got != cpTDirect {
		t.Fatalf("DNS-time Match(%s) = %v, want direct", cpAPIHost, got)
	}
	if got := re.Match(cpAPISubHost); got != cpTDirect {
		t.Fatalf("DNS-time Match(%s) = %v, want direct (suffix)", cpAPISubHost, got)
	}
	if got := re.Match(cpResolvedIP); got != cpTDirect {
		t.Fatalf("conn-time Match(%s) = %v, want direct (DNS-learned pin)", cpResolvedIP, got)
	}
}

func TestControlPlaneDirect_GlobalAllProxy(t *testing.T) {
	// Worst case: a match:all proxy catch-all → DisableSniff()=true (SNI sniffing
	// off), so the DNS-learned pin is the ONLY path that can route the API direct.
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			cpDirectRoute(cpAPIHost),
			{Via: cpServerURL, Match: rule.MatchConfig{All: true}},
		},
	}
	if !client.IsAllProxy() {
		t.Fatal("precondition: expected config.IsAllProxy=true for global preset")
	}
	assertControlPlaneDirect(t, client)

	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()

	// The exception route must NOT re-enable sniffing — keeping it off is what
	// prevents BitTorrent being forced direct (tunnel leak) in global mode. The
	// control-plane host still routes direct via the DNS pin, not via sniffing.
	if !re.DisableSniff() {
		t.Fatal("DisableSniff()=false: a control-plane exception must not re-enable sniffing")
	}
	// Unrelated traffic still tunnels (the prefix is specific, not blanket-direct).
	if got := re.Match(cpUnrelatedIP); got != cpTProxy {
		t.Fatalf("Match(%s) = %v, want proxy (fallback intact)", cpUnrelatedIP, got)
	}
}

func TestControlPlaneDirect_BypassCN(t *testing.T) {
	// cn-bypass also emits match:{all:true} → allProxy. Region/names bundles are
	// absent here (engine no-ops them); the control-plane host must still win.
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			cpDirectRoute(cpAPIHost),
			{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
			{Via: "reject", Match: rule.MatchConfig{Names: []string{"tencent-overseas"}}},
			{Via: cpServerURL, Match: rule.MatchConfig{All: true}},
		},
	}
	assertControlPlaneDirect(t, client)
}

func TestControlPlaneDirect_HomeProxy(t *testing.T) {
	// home_proxy: non-all proxy default (match:{}) → allProxy=false anyway, but
	// the control-plane host must still route direct.
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			cpDirectRoute(cpAPIHost),
			{Via: "k2p://home", Match: rule.MatchConfig{Preset: "cn-access"}},
			{Via: cpServerURL, Match: rule.MatchConfig{}},
		},
	}
	assertControlPlaneDirect(t, client)
}

// TestControlPlaneDirect_TrueAllProxyStillSkips guards the heap-bounding
// optimization: a genuine all-proxy config (no exception routes) must NOT pin
// DNS-learned rules — len(e.routes) == 0 keeps the skip.
func TestControlPlaneDirect_TrueAllProxyStillSkips(t *testing.T) {
	client := config.ClientFromURL(cpServerURL) // produces a single match:all proxy route
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()
	if !re.DisableSniff() {
		t.Fatal("precondition: expected DisableSniff=true")
	}
	// Match resolves to proxy; the pin attempt must be a no-op (still proxy).
	if got := learn(re, "example.com", cpResolvedIP); got != cpTProxy {
		t.Fatalf("Match(example.com) = %v, want proxy", got)
	}
	if got := re.Match(cpResolvedIP); got != cpTProxy {
		t.Fatalf("conn-time Match(%s) = %v, want proxy (no pin in true all-proxy)", cpResolvedIP, got)
	}
}
