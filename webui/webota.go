//go:build linux

package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LinuxBridgeVersion is the bridge API version compiled into this shell,
// compared against the manifest's min_bridge gate before applying a web
// OTA bundle. Bump when the Linux daemon's webapp-facing surface
// (daemon HTTP actions, webui endpoints) changes incompatibly.
const LinuxBridgeVersion = 1

// webOTAPublicKey is the minisign public key verifying web OTA bundles.
// Same key pair as the Tauri desktop updater — this is the key line of
// base64-decoding desktop/src-tauri/tauri.conf.json plugins.updater.pubkey
// (key ID 40D7545FD7CEDE83).
const webOTAPublicKey = "RWSD3s7XX1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE"

// maxWebBundleSize caps the web.zip download as defense against a
// compromised manifest pointing at a huge file.
const maxWebBundleSize = 200 << 20 // 200 MB

// WebOTAManifest mirrors {CDN}/kaitu/web/latest.json. Only fields the
// Linux consumer reads are declared; unknown fields (released_at,
// min_native, min_desktop) are ignored.
type WebOTAManifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`  // relative to the manifest's own directory (<endpoint>/web/), e.g. "0.4.9.1234/web.zip"
	Hash      string `json:"hash"` // "sha256:<hex>"
	Size      int64  `json:"size"`
	Sig       string `json:"sig"` // base64-encoded minisign signature of web.zip
	MinLinux  string `json:"min_linux"`
	MinBridge int    `json:"min_bridge"`
}

// WebOTAConfig parameterizes the web OTA poller, mirroring UpgradeConfig:
// each consumer chooses its own endpoints, state directory, and versions.
type WebOTAConfig struct {
	// Endpoints are brand CDN base URLs without trailing slash, e.g.
	// "https://dl.kaitu.io/kaitu". The manifest lives at
	// <endpoint>/web/latest.json; its url field is relative to that
	// manifest directory, so downloads resolve as <endpoint>/web/<url>
	// (matching the shipped mobile native resolveDownloadURL). Tried in
	// order; first successful manifest wins.
	Endpoints []string

	// Dir is the web OTA state directory (e.g. /etc/kaitu/web-ota); the
	// poller manages pending/, current/, previous/ beneath it.
	Dir string

	// ShellVersion is the running k2 binary version: the min_linux gate
	// input and the local-version fallback when current/version.txt is
	// absent (same fallback semantics as the mobile web OTA).
	ShellVersion string

	// PublicKey is the minisign public key (base64 key line). Empty
	// selects webOTAPublicKey; tests inject a generated key.
	PublicKey string

	// InitialDelay and Interval control the polling loop; zero values
	// default to 10s and 30min.
	InitialDelay time.Duration
	Interval     time.Duration
}

// WebOTA polls the web OTA manifest and atomically applies newer UI
// bundles under cfg.Dir.
type WebOTA struct {
	cfg    WebOTAConfig
	client *http.Client
}

// NewWebOTA constructs a poller, applying config defaults.
func NewWebOTA(cfg WebOTAConfig) *WebOTA {
	if cfg.PublicKey == "" {
		cfg.PublicKey = webOTAPublicKey
	}
	if cfg.InitialDelay == 0 {
		cfg.InitialDelay = 10 * time.Second
	}
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Minute
	}
	return &WebOTA{cfg: cfg, client: &http.Client{Timeout: 5 * time.Minute}}
}

// CurrentDir is the directory the webapp handler serves as its disk
// override (NewWebappHandlerWithOverride).
func (o *WebOTA) CurrentDir() string { return filepath.Join(o.cfg.Dir, "current") }

// fetchManifest tries each endpoint in order, returning the first
// successfully decoded manifest and the endpoint that served it.
func (o *WebOTA) fetchManifest() (*WebOTAManifest, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for _, ep := range o.cfg.Endpoints {
		m, err := fetchManifestFrom(client, ep+"/web/latest.json")
		if err != nil {
			lastErr = err
			continue
		}
		return m, ep, nil
	}
	return nil, "", fmt.Errorf("all web OTA endpoints failed: %w", lastErr)
}

func fetchManifestFrom(client *http.Client, url string) (*WebOTAManifest, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	var m WebOTAManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRequestBody)).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest from %s: %w", url, err)
	}
	if m.Version == "" || m.URL == "" {
		return nil, fmt.Errorf("manifest from %s missing version/url", url)
	}
	return &m, nil
}

// localVersion is the currently-applied web bundle version:
// current/version.txt when present, else the shell binary version.
func (o *WebOTA) localVersion() string {
	data, err := os.ReadFile(filepath.Join(o.CurrentDir(), "version.txt"))
	if err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}
	return o.cfg.ShellVersion
}

// shouldApply runs the three gates in order, returning a stable reason
// string for logging.
func (o *WebOTA) shouldApply(m *WebOTAManifest) (bool, string) {
	if !meetsMinVersion(m.MinLinux, o.cfg.ShellVersion) {
		return false, "shell below min_linux"
	}
	if !meetsMinBridge(m.MinBridge, LinuxBridgeVersion) {
		return false, "bridge below min_bridge"
	}
	if !isNewerVersion(m.Version, o.localVersion()) {
		return false, "not newer than local"
	}
	return true, "newer"
}
