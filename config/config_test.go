package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

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
server: "k2v5://udid:token@1.2.3.4:443"
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
server: "k2v5://udid:token@1.2.3.4:443"
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
	cfg := &ClientConfig{
		Listen: "127.0.0.1:1777",
		Server: "k2v5://udid:token@1.2.3.4:443",
		Mode:   "tun",
		Proxy:  ProxyConfig{Listen: "127.0.0.1:1080"},
		DNS:    DNSConfig{Direct: []string{"114.114.114.114:53"}, Proxy: []string{"8.8.8.8:53"}},
		Rule:   RuleConfig{Global: true, CacheDir: "/tmp/k2rule"},
		Log:    LogConfig{Level: "info", Output: "stderr"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify JSON uses snake_case keys matching YAML tags.
	var raw map[string]any
	json.Unmarshal(data, &raw)
	for _, key := range []string{"listen", "server", "mode", "proxy", "dns", "rule", "log"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q, not found in %s", key, string(data))
		}
	}

	var got ClientConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(cfg, &got) {
		t.Fatalf("round-trip mismatch:\n  original: %+v\n  decoded:  %+v", cfg, got)
	}
}

func TestClientConfigJSONYAMLEquivalence(t *testing.T) {
	// Build config from YAML file.
	yamlContent := `
server: "k2v5://udid:token@1.2.3.4:443"
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
		"server": "k2v5://udid:token@1.2.3.4:443",
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

	if !reflect.DeepEqual(yamlCfg, &jsonCfg) {
		t.Fatalf("YAML and JSON configs differ:\n  YAML: %+v\n  JSON: %+v", yamlCfg, jsonCfg)
	}
}

func TestTunConfig_JSONRoundTrip(t *testing.T) {
	cfg := &ClientConfig{
		Server: "k2v5://udid:token@1.2.3.4:443",
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
server: "k2v5://udid:token@1.2.3.4:443"
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
	yamlData := []byte("block_quic: true\nserver: k2v5://test\n")
	var cfg ClientConfig
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.BlockQUIC {
		t.Error("BlockQUIC should be true after YAML unmarshal")
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
