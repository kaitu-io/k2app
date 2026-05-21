//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/kaitu-io/k2/config"
)


func TestSetGatewayDefaults(t *testing.T) {
	cfg := &GatewayConfig{}
	SetGatewayDefaults(cfg)
	if cfg.ListenPort != DefaultListenPort {
		t.Errorf("ListenPort = %d, want %d", cfg.ListenPort, DefaultListenPort)
	}
	if cfg.Listen != DefaultGatewayAddr {
		t.Errorf("Listen = %q, want %q", cfg.Listen, DefaultGatewayAddr)
	}
}

// DNS redirect is the default behavior for k2r — without it, dnsmasq's upstream
// queries leak directly out the WAN since PREROUTING-only TPROXY can't catch
// locally-generated packets. Default-on closes that leak; explicit opt-out is
// reserved for diagnostic builds and not exposed today.
func TestSetGatewayDefaults_EnablesDNSRedirect(t *testing.T) {
	cfg := &GatewayConfig{}
	SetGatewayDefaults(cfg)
	if !cfg.DNSRedirect {
		t.Error("DNSRedirect = false, want true (default-on to prevent dnsmasq DNS leak)")
	}
	if cfg.DNSPort == 0 {
		t.Error("DNSPort = 0, want non-zero (ListenPort+1)")
	}
	if cfg.DNSPort != cfg.ListenPort+1 {
		t.Errorf("DNSPort = %d, want ListenPort+1 = %d", cfg.DNSPort, cfg.ListenPort+1)
	}
}

func TestSetGatewayDefaults_PreservesCustomPort(t *testing.T) {
	cfg := &GatewayConfig{ListenPort: 9999}
	SetGatewayDefaults(cfg)
	if cfg.ListenPort != 9999 {
		t.Errorf("ListenPort = %d, want 9999 (preserved)", cfg.ListenPort)
	}
}

func TestSetGatewayDefaults_OverridesDaemonPort(t *testing.T) {
	cfg := &GatewayConfig{}
	cfg.Listen = "127.0.0.1:1777" // daemon port
	SetGatewayDefaults(cfg)
	if cfg.Listen != DefaultGatewayAddr {
		t.Errorf("Listen = %q, want %q (should override daemon port)", cfg.Listen, DefaultGatewayAddr)
	}
}

func TestGatewayFromURL(t *testing.T) {
	cfg := GatewayFromURL("k2v5://udid:token@example.com:443/?ech=abc")
	if cfg.Server != "k2v5://udid:token@example.com:443/?ech=abc" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.ListenPort != DefaultListenPort {
		t.Errorf("ListenPort = %d, want %d", cfg.ListenPort, DefaultListenPort)
	}
}

func TestLoadGateway(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "k2r.yml")
	content := `routes:
  - via: "k2v5://udid:token@example.com:443/?ech=abc"
    match:
      all: true
listen_port: 8888
lan_subnets:
  - "192.168.1.0/24"
dns:
  direct:
    - "223.5.5.5:53"
  proxy:
    - "8.8.8.8:53"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGateway(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPort != 8888 {
		t.Errorf("ListenPort = %d, want 8888", cfg.ListenPort)
	}
	if len(cfg.LANSubnets) != 1 || cfg.LANSubnets[0] != "192.168.1.0/24" {
		t.Errorf("LANSubnets = %v", cfg.LANSubnets)
	}
	if cfg.Server != "k2v5://udid:token@example.com:443/?ech=abc" {
		t.Errorf("Server = %q (derived from routes)", cfg.Server)
	}
}

func TestLoadGateway_NotFound(t *testing.T) {
	_, err := LoadGateway("/nonexistent/path.yml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// TestExtractDialTargets pins the contract that resolveExcludeIPs depends on:
// only wire-outbound (k2v5/k2p) routes contribute targets. k2subs:// MUST NOT
// — it is unresolved at this point; its host is the subscription API, not the
// wire server. "direct"/"reject" sentinels are skipped. ip=/ip6= URL params
// flow through as hintedIPs so the exclude path can skip DNS, matching the
// engine's resolveDialAddr behavior.
func TestExtractDialTargets(t *testing.T) {
	tests := []struct {
		name    string
		routes  []config.RouteConfig
		want    []dialTarget
		wantErr bool
	}{
		{
			name:   "single k2v5 route, no ip hint",
			routes: []config.RouteConfig{{Via: "k2v5://u:t@picked.example:443/?ech=x"}},
			want:   []dialTarget{{host: "picked.example"}},
		},
		{
			name: "two k2v5 routes",
			routes: []config.RouteConfig{
				{Via: "k2v5://u:t@a.example:443/?ech=x"},
				{Via: "k2v5://u:t@b.example:443/"},
			},
			want: []dialTarget{
				{host: "a.example"},
				{host: "b.example"},
			},
		},
		{
			name: "direct + k2subs are skipped",
			routes: []config.RouteConfig{
				{Via: "direct"},
				{Via: "k2subs://api.example/"},
			},
			want: nil,
		},
		{
			name: "k2v5 + direct keeps wire only",
			routes: []config.RouteConfig{
				{Via: "k2v5://u:t@a.example:443/?ech=x"},
				{Via: "direct"},
			},
			want: []dialTarget{{host: "a.example"}},
		},
		{
			name:   "ip= hint populates hintedIPs",
			routes: []config.RouteConfig{{Via: "k2v5://u:t@host.example:443/?ech=x&ip=1.2.3.4"}},
			want:   []dialTarget{{host: "host.example", hintedIPs: []string{"1.2.3.4"}}},
		},
		{
			name:   "ip6= hint populates hintedIPs",
			routes: []config.RouteConfig{{Via: "k2v5://u:t@host.example:443/?ech=x&ip6=2001:db8::1"}},
			want:   []dialTarget{{host: "host.example", hintedIPs: []string{"2001:db8::1"}}},
		},
		{
			name:   "ip= and ip6= both flow through",
			routes: []config.RouteConfig{{Via: "k2v5://u:t@host.example:443/?ech=x&ip=1.2.3.4&ip6=2001:db8::1"}},
			want:   []dialTarget{{host: "host.example", hintedIPs: []string{"1.2.3.4", "2001:db8::1"}}},
		},
		{
			name: "subscription-emitted form (no slash before ?, with ip= hint)",
			routes: []config.RouteConfig{
				{Via: "k2v5://u:t@www.sichuan.people.cn:443?ech=AAA=&pin=sha256:abc&hop=40000-40019&ip=97.64.25.90"},
			},
			want: []dialTarget{{host: "www.sichuan.people.cn", hintedIPs: []string{"97.64.25.90"}}},
		},
		{
			name:    "malformed k2v5 returns error (no port)",
			routes:  []config.RouteConfig{{Via: "k2v5://u:t@host.example/?ech=x"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractDialTargets(tt.routes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractDialTargets err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractDialTargets(%+v) = %+v, want %+v", tt.routes, got, tt.want)
			}
		})
	}
}

// TestResolveExcludeIPs_HintedIPSkipsDNS proves that targets carrying
// hintedIPs do not trigger net.LookupHost. We use an RFC 6761 .invalid host
// — if DNS were attempted it would NXDOMAIN and the call would error out.
func TestResolveExcludeIPs_HintedIPSkipsDNS(t *testing.T) {
	targets := []dialTarget{
		{host: "this-must-never-resolve.invalid", hintedIPs: []string{"203.0.113.7"}},
	}
	got, err := resolveExcludeIPs(targets, nil)
	if err != nil {
		t.Fatalf("resolveExcludeIPs returned error (DNS was attempted on hinted target?): %v", err)
	}
	if !slices.Contains(got, "203.0.113.7") {
		t.Errorf("expected 203.0.113.7 in exclude list, got %v", got)
	}
}
