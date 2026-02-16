package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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
	if cfg.Log.Output != "stderr" {
		t.Errorf("Log.Output = %q, want stderr", cfg.Log.Output)
	}
}
