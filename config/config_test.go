package config

import (
	"os"
	"path/filepath"
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
