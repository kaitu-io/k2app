//go:build linux

package webui

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aead.dev/minisign"

	"github.com/kaitu-io/k2/safego"
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

// apply downloads, verifies (sha256 then minisign), unpacks into
// pending/, stamps version.txt, and atomically swaps pending → current.
// Any failure leaves current/ untouched.
func (o *WebOTA) apply(m *WebOTAManifest, endpoint string) error {
	if err := os.MkdirAll(o.cfg.Dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", o.cfg.Dir, err)
	}
	zipPath := filepath.Join(o.cfg.Dir, "web-"+m.Version+".zip")
	defer os.Remove(zipPath)

	// m.URL is relative to the manifest's directory: <endpoint>/web/.
	if err := o.download(endpoint+"/web/"+m.URL, zipPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := verifySHA256(zipPath, m.Hash); err != nil {
		return err
	}
	if err := verifyMinisign(zipPath, m.Sig, o.cfg.PublicKey); err != nil {
		return err
	}

	pending := filepath.Join(o.cfg.Dir, "pending")
	if err := os.RemoveAll(pending); err != nil {
		return fmt.Errorf("clear pending: %w", err)
	}
	if err := unzipTo(zipPath, pending); err != nil {
		os.RemoveAll(pending)
		return fmt.Errorf("unzip: %w", err)
	}
	if _, err := os.Stat(filepath.Join(pending, "index.html")); err != nil {
		os.RemoveAll(pending)
		return fmt.Errorf("bundle missing index.html: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pending, "version.txt"), []byte(m.Version+"\n"), 0644); err != nil {
		os.RemoveAll(pending)
		return fmt.Errorf("write version.txt: %w", err)
	}
	return o.swap(pending)
}

// download fetches url into dst, capped at maxWebBundleSize.
func (o *WebOTA) download(url, dst string) error {
	resp, err := o.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, maxWebBundleSize))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// verifySHA256 checks the file at path against a "sha256:<hex>" manifest
// hash. The prefix is mandatory — a bare hex hash is rejected.
func verifySHA256(path, want string) error {
	hexWant, ok := strings.CutPrefix(want, "sha256:")
	if !ok {
		return fmt.Errorf("manifest hash %q missing sha256: prefix", want)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, hexWant) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", hexWant, got)
	}
	return nil
}

// verifyMinisign verifies a base64-encoded minisign signature (the
// content of a .minisig file, base64d — same convention as the Tauri
// updater's signature field) over the file at path. Verification is
// mandatory: an empty sig is an error, not a skip.
func verifyMinisign(path, sigB64, pubKey string) error {
	if sigB64 == "" {
		return fmt.Errorf("manifest has no minisign sig")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode sig: %w", err)
	}
	var pk minisign.PublicKey
	if err := pk.UnmarshalText([]byte(pubKey)); err != nil {
		return fmt.Errorf("parse minisign public key: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !minisign.Verify(pk, data, sigBytes) {
		return fmt.Errorf("minisign verification failed")
	}
	return nil
}

// unzipTo extracts a zip whose top level is the dist content into dst,
// rejecting entries that would escape dst (zip-slip).
func unzipTo(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	cleanDst := filepath.Clean(dst)
	for _, f := range zr.File {
		target := filepath.Join(cleanDst, filepath.FromSlash(f.Name))
		if target != cleanDst && !strings.HasPrefix(target, cleanDst+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry escapes destination: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// swap promotes pending → current, keeping the old current as previous.
// Renames are atomic on the same filesystem; if the final promote fails
// the old current is restored best-effort.
func (o *WebOTA) swap(pending string) error {
	current := o.CurrentDir()
	previous := filepath.Join(o.cfg.Dir, "previous")
	os.RemoveAll(previous)
	hadCurrent := false
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, previous); err != nil {
			return fmt.Errorf("rotate current: %w", err)
		}
		hadCurrent = true
	}
	if err := os.Rename(pending, current); err != nil {
		if hadCurrent {
			os.Rename(previous, current) // best-effort restore
		}
		return fmt.Errorf("promote pending: %w", err)
	}
	return nil
}

// checkOnce runs one poll cycle: manifest → gates → apply.
func (o *WebOTA) checkOnce() error {
	m, endpoint, err := o.fetchManifest()
	if err != nil {
		return err
	}
	ok, reason := o.shouldApply(m)
	if !ok {
		slog.Debug("webui: web OTA skipped", "remote", m.Version, "local", o.localVersion(), "reason", reason)
		return nil
	}
	from := o.localVersion()
	if err := o.apply(m, endpoint); err != nil {
		return fmt.Errorf("apply %s: %w", m.Version, err)
	}
	slog.Info("DIAG: webota-applied", "from", from, "to", m.Version)
	return nil
}

// Start launches the polling loop and returns immediately. The loop
// waits cfg.InitialDelay, then polls every cfg.Interval until ctx is
// cancelled. A failed cycle logs once (30-min cadence keeps DIAG volume
// bounded) and retries on the next tick.
func (o *WebOTA) Start(ctx context.Context) {
	safego.Go(func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(o.cfg.InitialDelay):
		}
		for {
			if err := o.checkOnce(); err != nil {
				slog.Warn("DIAG: webota-fail", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(o.cfg.Interval):
			}
		}
	})
}
