//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kaitu-io/k2/config"
)

func TestExtractServerHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"k2v5://example.com:443/path?token=x", "example.com"},
		{"k2v5://user:pass@example.com:443/path", "example.com"},
		{"k2v5://1.2.3.4:443/path", "1.2.3.4"},
		{"k2v5://[::1]:443/path", "::1"},
		{"k2v5://host:443", "host"},
		{"k2v5://udid:token@my-server.example.com:443/?ech=abc", "my-server.example.com"},
	}
	for _, tt := range tests {
		got := extractServerHost(tt.url)
		if got != tt.want {
			t.Errorf("extractServerHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

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

// TestExtractRouteHosts pins the contract that resolveExcludeIPs depends on:
// only wire-outbound (k2v5/k2p) routes contribute hosts to the TPROXY exclude
// list. k2subs:// must NOT — it is unresolved at this point and resolving it
// would yield the subscription API host, not the actual wire server. "direct"
// and "reject" sentinels are skipped.
func TestExtractRouteHosts(t *testing.T) {
	tests := []struct {
		name   string
		routes []config.RouteConfig
		want   []string
	}{
		{
			name:   "single k2v5 route",
			routes: []config.RouteConfig{{Via: "k2v5://u:t@picked.example/?ech=x"}},
			want:   []string{"picked.example"},
		},
		{
			name: "two k2v5 routes",
			routes: []config.RouteConfig{
				{Via: "k2v5://u:t@a.example:443/?ech=x"},
				{Via: "k2v5://u:t@b.example:443"},
			},
			want: []string{"a.example", "b.example"},
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
				{Via: "k2v5://u:t@a.example/?ech=x"},
				{Via: "direct"},
			},
			want: []string{"a.example"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRouteHosts(tt.routes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractRouteHosts(%+v) = %v, want %v", tt.routes, got, tt.want)
			}
		})
	}
}
