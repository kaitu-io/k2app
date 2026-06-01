package rule

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DefaultSources is the list of CDN sources for all.krs.tar.gz downloads
// (the single .krs archive; see ensure.go tarballName). Happy Eyeballs races
// all sources in parallel — fastest wins. GitHub is listed first as the
// authoritative origin; jsdelivr/ghproxy mirrors provide acceleration and
// censorship resilience in CN. The "@release" path is the orphan release
// branch in kaitu-io/k2-rules that holds the published artifacts (no source).
var DefaultSources = []string{
	// GitHub Releases (canonical origin)
	"https://github.com/kaitu-io/k2-rules/releases/latest/download/",
	// GitHub HTTPS proxies (preserve the /releases/latest/download/ path)
	"https://ghfast.top/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
	"https://gh-proxy.com/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
	// jsdelivr @release branch (4 global POPs).
	// Bounded staleness: s-maxage=43200 (≤12h behind origin).
	// Two third-party "jsdelivr-compatible" mirrors (cdn.jsdmirror.com,
	// jsd.onmicrosoft.cn) were removed 2026-05-24 after Age headers
	// showed ~25-day cache staleness — they don't honor revalidation
	// and served pre-merge bundles in production.
	"https://cdn.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
	"https://fastly.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
	"https://testingcf.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
	"https://gcore.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
}

// downloadResult describes the outcome of a single HTTP fetch attempt.
type downloadResult int

const (
	downloadResultOK          downloadResult = iota
	downloadResultNotModified                // 304
	downloadResultFailed
)

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

