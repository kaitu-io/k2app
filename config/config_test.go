package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

// TestClientConfig_AppBypass_JSONRoundtrip locks the wire format between
// webapp (TS) and Go. Field drift here is the #1 way App Bypass v2 can
// silently break — TS sends `app_bypass.process_adds` but Go expects
// `app_bypass.processAdds`, and you'd never notice in unit tests because
// both sides default to nil. This test asserts the exact snake_case keys
// the webapp emits in webapp/src/stores/config.store.ts buildConnectConfig.
func TestClientConfig_AppBypass_JSONRoundtrip(t *testing.T) {
	cfg := ClientConfig{
		AppBypass: &AppBypassConfig{
			Region:      "cn",
			ProcessAdds: []string{"Steam.app", "Battle.net Helper"},
			PackageAdds: []string{"com.epicgames.unrealtournament"},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify wire-level snake_case keys match webapp's emit.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	ab, ok := raw["app_bypass"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level 'app_bypass' key, got: %s", string(data))
	}
	for _, key := range []string{"region", "process_adds", "package_adds"} {
		if _, ok := ab[key]; !ok {
			t.Errorf("expected app_bypass.%s, missing in: %v", key, ab)
		}
	}
	for _, badKey := range []string{"processAdds", "packageAdds", "Region"} {
		if _, ok := ab[badKey]; ok {
			t.Errorf("camelCase/PascalCase leaked: app_bypass.%s present", badKey)
		}
	}

	// Roundtrip back into struct.
	var got ClientConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal struct: %v", err)
	}
	if got.AppBypass == nil {
		t.Fatal("got.AppBypass is nil")
	}
	if got.AppBypass.Region != "cn" {
		t.Errorf("region = %q, want cn", got.AppBypass.Region)
	}
	if len(got.AppBypass.ProcessAdds) != 2 || got.AppBypass.ProcessAdds[0] != "Steam.app" {
		t.Errorf("process_adds = %v", got.AppBypass.ProcessAdds)
	}
	if len(got.AppBypass.PackageAdds) != 1 || got.AppBypass.PackageAdds[0] != "com.epicgames.unrealtournament" {
		t.Errorf("package_adds = %v", got.AppBypass.PackageAdds)
	}
}

// TestClientConfig_AppBypass_OmitemptySafety verifies that nil and zero-value
// AppBypass do not pollute the wire payload. Old webapps that don't set the
// field must produce JSON that old daemons (and Go json.Unmarshal in general)
// accept without errors, and new daemons must treat absent AppBypass as
// "no smart bypass requested" without crashing.
func TestClientConfig_AppBypass_OmitemptySafety(t *testing.T) {
	t.Run("nil pointer omitted", func(t *testing.T) {
		cfg := ClientConfig{Mode: "tun"}
		data, _ := json.Marshal(cfg)
		var raw map[string]any
		json.Unmarshal(data, &raw)
		if _, present := raw["app_bypass"]; present {
			t.Errorf("nil AppBypass should be omitted, got: %s", string(data))
		}
	})

	t.Run("unmarshal missing field yields nil", func(t *testing.T) {
		var cfg ClientConfig
		if err := json.Unmarshal([]byte(`{"mode":"tun"}`), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.AppBypass != nil {
			t.Errorf("missing app_bypass should yield nil, got %+v", cfg.AppBypass)
		}
	})

	t.Run("unknown fields tolerated (forward compat)", func(t *testing.T) {
		// Future webapps may add fields like enabled, version, etc. Go's default
		// json.Unmarshal silently ignores unknown fields, but assert here so a
		// future strict-decode regression is caught immediately.
		var cfg ClientConfig
		err := json.Unmarshal([]byte(`{
			"app_bypass": {
				"region": "cn",
				"future_field": "ignored",
				"enabled": true
			}
		}`), &cfg)
		if err != nil {
			t.Errorf("forward compat: unknown fields rejected: %v", err)
		}
		if cfg.AppBypass == nil || cfg.AppBypass.Region != "cn" {
			t.Errorf("known fields lost: %+v", cfg.AppBypass)
		}
	})

	t.Run("empty arrays decoded as nil-or-empty (both acceptable)", func(t *testing.T) {
		var cfg ClientConfig
		err := json.Unmarshal([]byte(`{
			"app_bypass": {"region": "cn", "process_adds": []}
		}`), &cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Go json decodes "[]" as zero-length slice, NOT nil. Either is fine for
		// downstream — buildAppBypassRoute uses len() not nil-check.
		if cfg.AppBypass.ProcessAdds == nil {
			// not necessarily wrong; nil is len==0
		}
		if len(cfg.AppBypass.ProcessAdds) != 0 {
			t.Errorf("expected zero-length process_adds, got %v", cfg.AppBypass.ProcessAdds)
		}
	})
}

// TestClientConfig_AppBypass_YAMLRoundtrip verifies daemon config-file parsing
// (used when k2 daemon loads -c config.yaml) preserves the same shape. This
// is more relevant for advanced users / self-hosters than the wire path,
// but the YAML tags share the same source-of-truth struct so a drift would
// surface here too.
func TestClientConfig_AppBypass_YAMLRoundtrip(t *testing.T) {
	yamlIn := `
app_bypass:
  region: ir
  process_adds:
    - Telegram.app
  package_adds:
    - org.telegram.messenger
`
	var cfg ClientConfig
	if err := yaml.Unmarshal([]byte(yamlIn), &cfg); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if cfg.AppBypass == nil || cfg.AppBypass.Region != "ir" {
		t.Errorf("region missing/wrong: %+v", cfg.AppBypass)
	}
	if len(cfg.AppBypass.ProcessAdds) != 1 || cfg.AppBypass.ProcessAdds[0] != "Telegram.app" {
		t.Errorf("process_adds: %v", cfg.AppBypass.ProcessAdds)
	}
}
