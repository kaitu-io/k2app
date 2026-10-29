package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
	"gopkg.in/yaml.v3"
)

func TestClientConfig_Defaults(t *testing.T) {
	cfg := ClientFromURL("k2v5://udid:token@1.2.3.4:443")

	if cfg.Mode != "tun" {
		t.Errorf("Mode = %q, want tun", cfg.Mode)
	}
	if cfg.Proxy.Listen != "127.0.0.1:1080" {
		t.Errorf("Proxy.Listen = %q, want 127.0.0.1:1080", cfg.Proxy.Listen)
	}
}

func TestClientConfig_ProxyMode(t *testing.T) {
	yaml := `
routes:
  - via: "k2v5://udid:token@1.2.3.4:443"
    match:
      all: true
mode: proxy
proxy:
  listen: "0.0.0.0:8080"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Mode != "proxy" {
		t.Errorf("Mode = %q, want proxy", cfg.Mode)
	}
	if cfg.Proxy.Listen != "0.0.0.0:8080" {
		t.Errorf("Proxy.Listen = %q, want 0.0.0.0:8080", cfg.Proxy.Listen)
	}
}

func TestServerConfig_Defaults(t *testing.T) {
	yaml := `
tls:
  cert: /etc/ssl/cert.pem
  key: /etc/ssl/key.pem
`
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Listen != ":443" {
		t.Errorf("Listen = %q, want :443", cfg.Listen)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
}

func TestServerConfig_ECH(t *testing.T) {
	// AC7: ServerConfig YAML supports ech.key_path and ech.public_name
	yaml := `
listen: ":8443"
tls:
  cert: /etc/ssl/cert.pem
  key: /etc/ssl/key.pem
ech:
  key_path: /etc/k2/ech.key
  public_name: cloudflare-ech.com
`
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ECH.KeyPath != "/etc/k2/ech.key" {
		t.Errorf("ECH.KeyPath = %q, want /etc/k2/ech.key", cfg.ECH.KeyPath)
	}
	if cfg.ECH.PublicName != "cloudflare-ech.com" {
		t.Errorf("ECH.PublicName = %q, want cloudflare-ech.com", cfg.ECH.PublicName)
	}
}

func TestDefaultServerDir(t *testing.T) {
	dir := defaultServerDir()
	if dir == "" {
		t.Fatal("defaultServerDir returned empty string")
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(dir, "k2s") {
			t.Errorf("Windows server dir should contain k2s, got %q", dir)
		}
	} else {
		if dir != "/etc/k2s" {
			t.Errorf("server dir should be /etc/k2s, got %q", dir)
		}
	}
}

func TestDefaultClientDir(t *testing.T) {
	dir := DefaultClientDir()
	if dir == "" {
		t.Fatal("DefaultClientDir returned empty string")
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(dir, "k2") {
			t.Errorf("Windows client dir should contain k2, got %q", dir)
		}
	} else {
		if dir != "/etc/k2" {
			t.Errorf("client dir should be /etc/k2, got %q", dir)
		}
	}
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Listen != ":443" {
		t.Errorf("Listen = %q, want :443", cfg.Listen)
	}
	if cfg.CertDir == "" {
		t.Error("CertDir should not be empty in default config")
	}
	if cfg.ECH.Provider != "local" {
		t.Errorf("ECH.Provider = %q, want local", cfg.ECH.Provider)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Output != "stderr" {
		t.Errorf("Log.Output = %q, want stderr", cfg.Log.Output)
	}
}

// TestServerDefaults_AuthCacheTTL15m: the auth cache TTL is aligned with the
// Phase 2 cookie TTL (15m) — spec §5.3. The revocation ceiling is "at most
// 15 minutes after Center's last endorsement".
func TestServerDefaults_AuthCacheTTL15m(t *testing.T) {
	cfg := DefaultServerConfig()
	if cfg.Auth.CacheTTL != 15*time.Minute {
		t.Fatalf("Auth.CacheTTL default = %v, want 15m", cfg.Auth.CacheTTL)
	}
}

func TestSetServerDefaults_CertDirDefault(t *testing.T) {
	// When CertDir is empty, setServerDefaults should populate it
	cfg := &ServerConfig{}
	setServerDefaults(cfg)

	if cfg.CertDir == "" {
		t.Error("CertDir should be set by setServerDefaults")
	}
}

func TestServerConfig_LocalRoutes(t *testing.T) {
	yamlContent := `
listen: ":443"
local_routes:
  "*.vpn.example.com": "127.0.0.1:8443"
  "oc.example.com": "127.0.0.1:10001"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	os.WriteFile(path, []byte(yamlContent), 0644)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.LocalRoutes) != 2 {
		t.Fatalf("LocalRoutes count = %d, want 2", len(cfg.LocalRoutes))
	}
	if cfg.LocalRoutes["*.vpn.example.com"] != "127.0.0.1:8443" {
		t.Errorf("wildcard route = %q, want 127.0.0.1:8443", cfg.LocalRoutes["*.vpn.example.com"])
	}
	if cfg.LocalRoutes["oc.example.com"] != "127.0.0.1:10001" {
		t.Errorf("exact route = %q, want 127.0.0.1:10001", cfg.LocalRoutes["oc.example.com"])
	}
}

func TestServerConfig_LocalRoutesEmpty(t *testing.T) {
	yamlContent := `
listen: ":443"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	os.WriteFile(path, []byte(yamlContent), 0644)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.LocalRoutes) != 0 {
		t.Errorf("LocalRoutes should be empty, got %d entries", len(cfg.LocalRoutes))
	}
}

func TestSetServerDefaults_CertDirPreserved(t *testing.T) {
	// When CertDir is already set, it should not be overwritten
	cfg := &ServerConfig{CertDir: "/custom/path"}
	setServerDefaults(cfg)

	if cfg.CertDir != "/custom/path" {
		t.Errorf("CertDir = %q, want /custom/path", cfg.CertDir)
	}
}

func TestConfigListenDefault(t *testing.T) {
	cfg := ClientFromURL("k2v5://udid:token@1.2.3.4:443")
	if cfg.Listen != "127.0.0.1:1777" {
		t.Errorf("Listen = %q, want 127.0.0.1:1777", cfg.Listen)
	}
}

func TestConfigListenFromYAML(t *testing.T) {
	yaml := `
listen: "0.0.0.0:1777"
routes:
  - via: "k2v5://udid:token@1.2.3.4:443"
    match:
      all: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Listen != "0.0.0.0:1777" {
		t.Errorf("Listen = %q, want 0.0.0.0:1777", cfg.Listen)
	}
}

// ==========================================================================
// Config-driven connect tests (F1)
// ==========================================================================

func TestClientConfigJSONRoundTrip(t *testing.T) {
	cfg := ClientFromURL("k2v5://udid:token@1.2.3.4:443")
	cfg.Listen = "127.0.0.1:1777"
	cfg.Mode = "tun"
	cfg.Proxy = ProxyConfig{Listen: "127.0.0.1:1080"}
	cfg.DNS = DNSConfig{Direct: []string{"114.114.114.114:53"}, Proxy: []string{"8.8.8.8:53"}}
	cfg.CacheDir = "/tmp/k2rule"
	cfg.Log = LogConfig{Level: "info", Output: "stderr"}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify JSON uses snake_case keys matching YAML tags.
	var raw map[string]any
	json.Unmarshal(data, &raw)
	for _, key := range []string{"listen", "mode", "proxy", "dns", "routes", "log"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q, not found in %s", key, string(data))
		}
	}
	// Server is derived (json:"-"), must NOT appear in JSON.
	if _, ok := raw["server"]; ok {
		t.Errorf("Server (derived) should not appear in JSON output")
	}

	var got ClientConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Server is derived (yaml:"-" json:"-"), so set it on got for comparison.
	SetDefaults(&got)
	if got.Server != cfg.Server {
		t.Errorf("server mismatch: got %q, want %q", got.Server, cfg.Server)
	}
	if got.Listen != cfg.Listen {
		t.Errorf("listen mismatch: got %q, want %q", got.Listen, cfg.Listen)
	}
	if got.CacheDir != cfg.CacheDir {
		t.Errorf("cache_dir mismatch: got %q, want %q", got.CacheDir, cfg.CacheDir)
	}
}

func TestClientConfigJSONYAMLEquivalence(t *testing.T) {
	// Build config from YAML file.
	yamlContent := `
routes:
  - via: "k2v5://udid:token@1.2.3.4:443"
    match:
      all: true
mode: proxy
proxy:
  listen: "0.0.0.0:8080"
dns:
  direct:
    - "114.114.114.114:53"
  proxy:
    - "8.8.8.8:53"
log:
  level: debug
  output: /var/log/k2.log
`
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	os.WriteFile(path, []byte(yamlContent), 0644)

	yamlCfg, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}

	// Build equivalent config from JSON.
	jsonContent := `{
		"routes": [{"via": "k2v5://udid:token@1.2.3.4:443", "match": {"all": true}}],
		"mode": "proxy",
		"proxy": {"listen": "0.0.0.0:8080"},
		"dns": {"direct": ["114.114.114.114:53"], "proxy": ["8.8.8.8:53"]},
		"log": {"level": "debug", "output": "/var/log/k2.log"}
	}`
	var jsonCfg ClientConfig
	if err := json.Unmarshal([]byte(jsonContent), &jsonCfg); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	SetDefaults(&jsonCfg)

	// Compare key fields (Server is derived, so compare it separately).
	if yamlCfg.Server != jsonCfg.Server {
		t.Fatalf("Server mismatch: YAML=%q, JSON=%q", yamlCfg.Server, jsonCfg.Server)
	}
	if yamlCfg.Mode != jsonCfg.Mode {
		t.Fatalf("Mode mismatch: YAML=%q, JSON=%q", yamlCfg.Mode, jsonCfg.Mode)
	}
	if yamlCfg.Routes[0].Via != jsonCfg.Routes[0].Via {
		t.Fatalf("Routes[0].Via mismatch: YAML=%q, JSON=%q", yamlCfg.Routes[0].Via, jsonCfg.Routes[0].Via)
	}
}

func TestTunConfig_JSONRoundTrip(t *testing.T) {
	cfg := &ClientConfig{
		Routes: []RouteConfig{{Via: "k2v5://udid:token@1.2.3.4:443", Match: rule.MatchConfig{All: true}}},
		Mode:   "tun",
		Tun:    TunConfig{IPv4: "10.0.0.2/24", IPv6: "fd00::2/128"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify JSON keys
	var raw map[string]any
	json.Unmarshal(data, &raw)
	tunRaw, ok := raw["tun"].(map[string]any)
	if !ok {
		t.Fatalf("expected tun key in JSON, got: %s", string(data))
	}
	if tunRaw["ipv4"] != "10.0.0.2/24" {
		t.Errorf("tun.ipv4 = %v, want 10.0.0.2/24", tunRaw["ipv4"])
	}
	if tunRaw["ipv6"] != "fd00::2/128" {
		t.Errorf("tun.ipv6 = %v, want fd00::2/128", tunRaw["ipv6"])
	}

	// Round-trip
	var got ClientConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tun.IPv4 != "10.0.0.2/24" || got.Tun.IPv6 != "fd00::2/128" {
		t.Errorf("round-trip: got Tun=%+v", got.Tun)
	}
}

func TestClientConfig_WithTun_YAML(t *testing.T) {
	yamlContent := `
routes:
  - via: "k2v5://udid:token@1.2.3.4:443"
    match:
      all: true
mode: tun
tun:
  ipv4: "10.0.0.2/24"
  ipv6: "fd00::2/128"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	os.WriteFile(path, []byte(yamlContent), 0644)

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Tun.IPv4 != "10.0.0.2/24" {
		t.Errorf("Tun.IPv4 = %q, want 10.0.0.2/24", cfg.Tun.IPv4)
	}
	if cfg.Tun.IPv6 != "fd00::2/128" {
		t.Errorf("Tun.IPv6 = %q, want fd00::2/128", cfg.Tun.IPv6)
	}
}

func TestClientConfig_EmptyTun_Defaults(t *testing.T) {
	// Empty tun config should be filled with platform defaults by SetDefaults.
	cfg := ClientFromURL("k2v5://udid:token@1.2.3.4:443")
	if cfg.Tun.IPv4 != DefaultTunIPv4 {
		t.Errorf("Tun.IPv4 = %q, want %q", cfg.Tun.IPv4, DefaultTunIPv4)
	}
	if cfg.Tun.IPv6 != DefaultTunIPv6 {
		t.Errorf("Tun.IPv6 = %q, want %q", cfg.Tun.IPv6, DefaultTunIPv6)
	}
}

func TestClientConfig_BlockQUIC_Default(t *testing.T) {
	// Zero value = false (QUIC allowed through tunnel)
	var cfg ClientConfig
	if cfg.BlockQUIC {
		t.Error("BlockQUIC zero value should be false")
	}
}

func TestClientConfig_BlockQUIC_YAML(t *testing.T) {
	yamlData := []byte("block_quic: true\nroutes:\n  - via: k2v5://test\n    match:\n      all: true\n")
	var cfg ClientConfig
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.BlockQUIC {
		t.Error("BlockQUIC should be true after YAML unmarshal")
	}
}

func TestIsOutboundURL_K2Subs(t *testing.T) {
	if !IsOutboundURL("k2subs://user:pass@host/path") {
		t.Error("k2subs should be an outbound URL")
	}
}

func TestIsAllProxy_K2Subs(t *testing.T) {
	cfg := &ClientConfig{
		Routes: []RouteConfig{
			{Via: "k2subs://user:pass@host/path", Match: rule.MatchConfig{All: true}},
		},
	}
	if !cfg.IsAllProxy() {
		t.Error("k2subs with all:true should be IsAllProxy")
	}
}

func TestClientFromSubsURL(t *testing.T) {
	cfg := ClientFromSubsURL("k2subs://alice:tok@relay.example.com/subs/team-a?refresh=600")
	if len(cfg.Routes) != 1 {
		t.Fatalf("Routes count = %d, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].Via != "k2subs://alice:tok@relay.example.com/subs/team-a?refresh=600" {
		t.Errorf("Via = %q", cfg.Routes[0].Via)
	}
	if cfg.Mode != "tun" {
		t.Errorf("Mode = %q, want tun", cfg.Mode)
	}
}

func TestSetDefaults_K2SubsServerEmpty(t *testing.T) {
	cfg := &ClientConfig{
		Routes: []RouteConfig{
			{Via: "k2subs://user:pass@host/path", Match: rule.MatchConfig{All: true}},
		},
	}
	SetDefaults(cfg)
	if cfg.Server != "" {
		t.Errorf("Server should be empty for k2subs routes, got %q", cfg.Server)
	}
}

func TestSetDefaultsExported(t *testing.T) {
	cfg := &ClientConfig{}
	SetDefaults(cfg)

	if cfg.Listen != "127.0.0.1:1777" {
		t.Errorf("Listen = %q, want 127.0.0.1:1777", cfg.Listen)
	}
	if cfg.Mode != "tun" {
		t.Errorf("Mode = %q, want tun", cfg.Mode)
	}
	if cfg.Proxy.Listen != "127.0.0.1:1080" {
		t.Errorf("Proxy.Listen = %q, want 127.0.0.1:1080", cfg.Proxy.Listen)
	}
	if len(cfg.DNS.Direct) == 0 {
		t.Error("DNS.Direct should have defaults")
	}
	if len(cfg.DNS.Proxy) == 0 {
		t.Error("DNS.Proxy should have defaults")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Output != "" {
		t.Errorf("Log.Output = %q, want empty (auto-detect via DefaultLogPath)", cfg.Log.Output)
	}
	if cfg.Tun.IPv4 != DefaultTunIPv4 {
		t.Errorf("Tun.IPv4 = %q, want %q", cfg.Tun.IPv4, DefaultTunIPv4)
	}
	if cfg.Tun.IPv6 != DefaultTunIPv6 {
		t.Errorf("Tun.IPv6 = %q, want %q", cfg.Tun.IPv6, DefaultTunIPv6)
	}
}

func TestClientConfig_RouteMatch_RegionAppsJSONRoundtrip(t *testing.T) {
	cfg := ClientConfig{
		Routes: []RouteConfig{
			{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
			{Via: "k2v5://x", Match: rule.MatchConfig{Apps: []string{"Firefox*", "Chrome.app"}}},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	routes, ok := raw["routes"].([]any)
	if !ok || len(routes) != 2 {
		t.Fatalf("expected routes array of len 2, got: %v", raw["routes"])
	}
	m0 := routes[0].(map[string]any)["match"].(map[string]any)
	if m0["region"] != "cn" {
		t.Errorf("routes[0].match.region = %v, want cn", m0["region"])
	}
	if _, present := m0["apps"]; present {
		t.Errorf("routes[0].match.apps should be omitted (empty), got: %v", m0["apps"])
	}
	m1 := routes[1].(map[string]any)["match"].(map[string]any)
	apps, _ := m1["apps"].([]any)
	if len(apps) != 2 || apps[0] != "Firefox*" {
		t.Errorf("routes[1].match.apps = %v, want [Firefox* Chrome.app]", apps)
	}
	if _, present := m1["region"]; present {
		t.Errorf("routes[1].match.region should be omitted (empty), got: %v", m1["region"])
	}

	var got ClientConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal struct: %v", err)
	}
	if got.Routes[0].Match.Region != "cn" {
		t.Errorf("Region roundtrip lost: %q", got.Routes[0].Match.Region)
	}
	if len(got.Routes[1].Match.Apps) != 2 || got.Routes[1].Match.Apps[1] != "Chrome.app" {
		t.Errorf("Apps roundtrip lost: %v", got.Routes[1].Match.Apps)
	}
}
