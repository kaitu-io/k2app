package rule

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultSources is the list of CDN sources for k2b bundle downloads.
// Happy Eyeballs races all sources in parallel — fastest wins.
// Order doesn't matter for performance, but GitHub direct is listed first
// as the authoritative source; CDN proxies provide China acceleration.
var DefaultSources = []string{
	"https://github.com/kaitu-io/k2-rules/releases/latest/download/",
	"https://ghfast.top/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
	"https://gh-proxy.com/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
}

// bundleFileMap maps BundleSet names to k2b file names. Aligned with the
// v2 schema shipped by kaitu-io/k2-rules (single "overseas" set, 2-set
// cn-direct, plus 13 additional per-country bundles). Old per-service names
// (google, facebook, ...) no longer exist. The "ir-blocked" / "ru-blocked"
// entries are reserved for a future force-proxy feature and live inside the
// same ir-direct / ru-direct bundles.
var bundleFileMap = map[string]string{
	"overseas": "overseas.k2b",
	"cn-sites": "cn-direct.k2b",
	"geoip-cn": "cn-direct.k2b",
	"ir-sites": "ir-direct.k2b",
	"geoip-ir": "ir-direct.k2b",
	"ir-blocked": "ir-direct.k2b",
	"ru-sites": "ru-direct.k2b",
	"geoip-ru": "ru-direct.k2b",
	"ru-blocked": "ru-direct.k2b",
	"tr-sites": "tr-direct.k2b",
	"geoip-tr": "tr-direct.k2b",
	"pk-sites": "pk-direct.k2b",
	"geoip-pk": "pk-direct.k2b",
	"vn-sites": "vn-direct.k2b",
	"geoip-vn": "vn-direct.k2b",
	"mm-sites": "mm-direct.k2b",
	"geoip-mm": "mm-direct.k2b",
	"eg-sites": "eg-direct.k2b",
	"geoip-eg": "eg-direct.k2b",
	"id-sites": "id-direct.k2b",
	"geoip-id": "id-direct.k2b",
	"sa-sites": "sa-direct.k2b",
	"geoip-sa": "sa-direct.k2b",
	"ae-sites": "ae-direct.k2b",
	"geoip-ae": "ae-direct.k2b",
	"th-sites": "th-direct.k2b",
	"geoip-th": "th-direct.k2b",
	"bd-sites": "bd-direct.k2b",
	"geoip-bd": "bd-direct.k2b",
	"by-sites": "by-direct.k2b",
	"geoip-by": "by-direct.k2b",
}

// BundlesForConfig returns a deduplicated list of k2b file names referenced
// by the given route match configs. Returns empty for All:true or inline-only
// matches.
func BundlesForConfig(matches []MatchConfig) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, m := range matches {
		if m.All {
			continue
		}
		names := ExpandMatch(m)
		for _, name := range names {
			if file, ok := bundleFileMap[name]; ok {
				if _, exists := seen[file]; !exists {
					seen[file] = struct{}{}
					result = append(result, file)
				}
			}
		}
	}
	if result == nil {
		return []string{}
	}
	return result
}

// downloadResult describes the outcome of a single HTTP fetch attempt.
type downloadResult int

const (
	downloadResultOK          downloadResult = iota
	downloadResultNotModified                // 304
	downloadResultFailed
)

// Download ensures all named k2b bundles exist in cacheDir. It creates the
// directory if needed and uses DefaultSources if sources is empty.
func Download(ctx context.Context, cacheDir string, bundles []string, sources []string) error {
	if len(bundles) == 0 {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("rule: create cache dir: %w", err)
	}
	if len(sources) == 0 {
		sources = DefaultSources
	}
	for _, name := range bundles {
		if err := downloadOne(ctx, cacheDir, name, sources); err != nil {
			return err
		}
	}
	return nil
}

// downloadOne downloads a single k2b bundle file with retry+backoff. If the
// file already exists on disk and all attempts fail, returns nil (use cached).
func downloadOne(ctx context.Context, cacheDir, name string, sources []string) error {
	dest := filepath.Join(cacheDir, name)
	attempt := 0
	for {
		res, err := raceDownload(ctx, dest, name, sources)
		if err == nil {
			switch res {
			case downloadResultOK, downloadResultNotModified:
				return nil
			}
		}
		// Check if cancelled.
		if ctx.Err() != nil {
			// If file already exists, use the cached copy.
			if _, statErr := os.Stat(dest); statErr == nil {
				return nil
			}
			return fmt.Errorf("rule: download %q cancelled: %w", name, ctx.Err())
		}
		// If file already exists, use cached copy on failure.
		if _, statErr := os.Stat(dest); statErr == nil {
			return nil
		}
		// Retry with backoff.
		wait := backoffDuration(attempt)
		attempt++
		select {
		case <-ctx.Done():
			return fmt.Errorf("rule: download %q cancelled: %w", name, ctx.Err())
		case <-time.After(wait):
		}
	}
}

// raceDownload runs Happy Eyeballs: spawns one goroutine per source, first
// HTTP 200 wins (others are cancelled). Uses If-Modified-Since from local
// file mtime. 304 means file is current. Per-race timeout is 30s.
func raceDownload(ctx context.Context, dest, name string, sources []string) (downloadResult, error) {
	raceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Determine local file mtime for conditional GET.
	var modTime time.Time
	if info, err := os.Stat(dest); err == nil {
		modTime = info.ModTime()
	}

	type result struct {
		res downloadResult
		err error
	}

	ch := make(chan result, len(sources))

	for _, src := range sources {
		src := src
		url := src + name
		go func() {
			data, res, err := fetchBundle(raceCtx, url, modTime)
			if err != nil || res == downloadResultFailed {
				ch <- result{res: downloadResultFailed, err: err}
				return
			}
			if res == downloadResultNotModified {
				ch <- result{res: downloadResultNotModified}
				return
			}
			// Atomic write: tmp → rename.
			tmp := dest + ".tmp"
			if werr := os.WriteFile(tmp, data, 0o644); werr != nil {
				ch <- result{res: downloadResultFailed, err: werr}
				return
			}
			if rerr := os.Rename(tmp, dest); rerr != nil {
				_ = os.Remove(tmp)
				ch <- result{res: downloadResultFailed, err: rerr}
				return
			}
			cancel() // signal other goroutines to stop
			ch <- result{res: downloadResultOK}
		}()
	}

	var lastErr error
	notModifiedCount := 0
	for i := 0; i < len(sources); i++ {
		r := <-ch
		switch r.res {
		case downloadResultOK:
			return downloadResultOK, nil
		case downloadResultNotModified:
			notModifiedCount++
			if notModifiedCount == len(sources) {
				return downloadResultNotModified, nil
			}
		default:
			if r.err != nil {
				lastErr = r.err
			}
		}
		// If at least one succeeded (OK) we already returned; check ctx.
		if raceCtx.Err() != nil && r.res != downloadResultOK {
			// Drain remaining but don't wait forever.
			continue
		}
	}
	if notModifiedCount > 0 {
		return downloadResultNotModified, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all sources failed")
	}
	return downloadResultFailed, lastErr
}

// fetchBundle performs a single HTTP GET with If-Modified-Since. Returns
// (data, downloadResultOK, nil) on 200, (nil, downloadResultNotModified, nil)
// on 304, or (nil, downloadResultFailed, err) on error.
func fetchBundle(ctx context.Context, url string, modTime time.Time) ([]byte, downloadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, downloadResultFailed, fmt.Errorf("rule: build request: %w", err)
	}
	if !modTime.IsZero() {
		req.Header.Set("If-Modified-Since", modTime.UTC().Format(http.TimeFormat))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, downloadResultFailed, fmt.Errorf("rule: fetch %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, downloadResultNotModified, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, downloadResultFailed, fmt.Errorf("rule: fetch %q: status %d", url, resp.StatusCode)
	}

	const maxBundleSize = 10 << 20 // 10 MB hard cap — prevents OOM on corrupted/malicious response
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleSize+1))
	if err != nil {
		return nil, downloadResultFailed, fmt.Errorf("rule: read body %q: %w", url, err)
	}
	if len(data) > maxBundleSize {
		return nil, downloadResultFailed, fmt.Errorf("rule: body too large from %q: %d bytes", url, len(data))
	}
	return data, downloadResultOK, nil
}

// backoffDuration returns 1s, 2s, 4s, 8s… capped at 60s.
func backoffDuration(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 60*time.Second {
		return 60 * time.Second
	}
	return d
}

