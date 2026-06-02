package rule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manifest mirrors k2-rules dist/manifest.json (schema v2). Only the fields
// the client consumes are modeled; unknown fields are ignored by json.
type Manifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Version       string                    `json:"version"` // RFC3339 UTC, monotonic
	Bundles       map[string]ManifestBundle `json:"bundles"`
}

// ManifestBundle is one region's published integrity metadata.
type ManifestBundle struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	RuleCount int    `json:"ruleCount"`
}

// parseVersion parses an RFC3339 manifest version. Unparseable → zero time
// (treated as the oldest possible), so a malformed/legacy version never wins
// newest-wins and never blocks an upgrade.
func parseVersion(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// remoteNewer reports whether remote version a is strictly newer than local
// version b. Numeric (time) comparison, not string — robust to format drift.
func remoteNewer(a, b string) bool {
	return parseVersion(a).After(parseVersion(b))
}

// embedSuffix marks a bundles.version written from the binary-embedded floor.
// Kept compatible with wasEmbedSeeded (embed.go), which substring-matches the
// embed marker so the background updater force-lifts the floor to the CDN.
const embedSuffix = " [embedded]"

// writeLocalVersion records the manifest version the cache currently holds.
// When embedded, appends the embed marker so wasEmbedSeeded detects the floor.
func writeLocalVersion(cacheDir, version string, embedded bool) error {
	line := version
	if embedded {
		line += embedSuffix
	}
	return os.WriteFile(filepath.Join(cacheDir, "bundles.version"), []byte(line+"\n"), 0o644)
}

// readLocalManifestVersion returns the manifest version recorded in
// bundles.version (embed marker stripped), or "" if absent/unreadable. This is
// the field the old design wrote but never read.
func readLocalManifestVersion(cacheDir string) string {
	data, err := os.ReadFile(filepath.Join(cacheDir, "bundles.version"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	s = strings.TrimSuffix(s, embedSuffix)
	return strings.TrimSpace(s)
}

const manifestName = "manifest.json"

// maxManifestSize caps the manifest body. It's a few hundred bytes per region;
// 1 MB is generous and guards against a hostile/broken response.
const maxManifestSize = 1 << 20

// fetchManifest GETs src+"manifest.json" and decodes it. src is a CDN base
// ending in "/".
func fetchManifest(ctx context.Context, src string) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src+manifestName, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rule: manifest %q: status %d", src, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("rule: manifest %q: %w", src, err)
	}
	return &m, nil
}

// manifestRaceTimeout bounds the whole manifest race. The manifest fetch is on
// the connect path for clients staler than shortTTL, so a slow/blocked mirror
// set must not eat the connect budget — past this bound we give up and the
// caller falls back to the local cache / legacy TTL.
const manifestRaceTimeout = 5 * time.Second

// raceManifest fetches the manifest from all sources concurrently and returns
// the one with the newest Version, plus the source URL it came from (so the
// tarball download can be pinned to the same mirror — avoiding cross-mirror
// propagation skew). Returns an error only if every source failed. Bounded by
// manifestRaceTimeout regardless of the caller's ctx deadline.
func raceManifest(ctx context.Context, sources []string) (*Manifest, string, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestRaceTimeout)
	defer cancel()
	type res struct {
		m   *Manifest
		src string
		err error
	}
	ch := make(chan res, len(sources))
	var wg sync.WaitGroup
	for _, src := range sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := fetchManifest(ctx, src)
			ch <- res{m: m, src: src, err: err}
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	var best *Manifest
	var bestSrc string
	var lastErr error
	for r := range ch {
		if r.err != nil || r.m == nil {
			lastErr = r.err
			continue
		}
		if best == nil || remoteNewer(r.m.Version, best.Version) {
			best, bestSrc = r.m, r.src
		}
	}
	if best == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("rule: all manifest sources failed")
		}
		return nil, "", lastErr
	}
	return best, bestSrc, nil
}

// verifyAgainstManifest checks every *.krs in dir against m: its sha256 must
// equal the manifest entry, and the manifest's ruleCount for that region must
// be > 0. A .krs with no manifest entry is allowed (manifest may legitimately
// not cover a locally-present region). Returns the first mismatch — the caller
// treats any error as "this download doesn't match the chosen manifest (stale
// mirror / corruption)" and keeps the prior cache.
func verifyAgainstManifest(dir string, m *Manifest) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".krs") {
			continue
		}
		region := strings.TrimSuffix(name, ".krs")
		mb, ok := m.Bundles[region]
		if !ok {
			continue
		}
		if mb.RuleCount == 0 {
			return fmt.Errorf("rule: manifest ruleCount=0 for region %q (empty bundle)", region)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != mb.SHA256 {
			slog.Warn("DIAG: rule-manifest-sha-mismatch", "region", region,
				"want", mb.SHA256, "got", got)
			return fmt.Errorf("rule: sha mismatch for region %q (mirror mid-propagation?)", region)
		}
	}
	return nil
}
