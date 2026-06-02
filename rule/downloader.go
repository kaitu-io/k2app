package rule

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DefaultSources is the list of CDN sources for krs.tar.gz downloads
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

	// Cap concurrent fetches so we never open all 8 TLS connections at once.
	// Slots refill as fetches finish/fail, so every source is still attempted
	// (CN punch-through preserved) — just never more than maxConcurrent in flight.
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)

	for i, src := range sources {
		i, src := i, src
		url := src + name
		go func() {
			// Acquire a concurrency slot first; bail if the race is already
			// cancelled (a winner renamed, or ctx/timeout fired) while waiting.
			select {
			case sem <- struct{}{}:
			case <-raceCtx.Done():
				ch <- result{res: downloadResultFailed, err: raceCtx.Err()}
				return
			}
			defer func() { <-sem }()

			// Stream straight to a unique temp file (avoids 8× in-memory buffers).
			// Keyed by the loop index — collision-free by construction, so two
			// sources sharing a path suffix can't race on the same temp path.
			tmp := fmt.Sprintf("%s.dl.%d", dest, i)
			res, err := fetchBundleToFile(raceCtx, url, modTime, tmp)
			if err != nil || res == downloadResultFailed {
				_ = os.Remove(tmp)
				ch <- result{res: downloadResultFailed, err: err}
				return
			}
			if res == downloadResultNotModified {
				ch <- result{res: downloadResultNotModified}
				return
			}
			// A late loser may overwrite dest with its own complete,
			// byte-identical copy — harmless (atomic rename, same artifact).
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

// fetchBundleToFile performs a single HTTP GET with If-Modified-Since and
// streams the body to dst+".part" (then renames to dst), capping at
// maxBundleSize. Streaming avoids holding the whole tarball in memory — the
// resident cost is the 32 KB io.Copy buffer, not the ~2.4 MB archive. Returns
// downloadResultOK on 200 (file written), downloadResultNotModified on 304
// (dst untouched), or downloadResultFailed on error (no dst left behind).
func fetchBundleToFile(ctx context.Context, url string, modTime time.Time, dst string) (downloadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return downloadResultFailed, fmt.Errorf("rule: build request: %w", err)
	}
	if !modTime.IsZero() {
		req.Header.Set("If-Modified-Since", modTime.UTC().Format(http.TimeFormat))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return downloadResultFailed, fmt.Errorf("rule: fetch %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return downloadResultNotModified, nil
	}
	if resp.StatusCode != http.StatusOK {
		return downloadResultFailed, fmt.Errorf("rule: fetch %q: status %d", url, resp.StatusCode)
	}

	const maxBundleSize = 10 << 20 // 10 MB hard cap — guards against a corrupt/hostile response
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return downloadResultFailed, fmt.Errorf("rule: open %q: %w", part, err)
	}
	// LimitReader cap+1 so an exactly-max body still distinguishes "too large".
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxBundleSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return downloadResultFailed, fmt.Errorf("rule: stream body %q: %w", url, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return downloadResultFailed, fmt.Errorf("rule: close %q: %w", part, closeErr)
	}
	if n > maxBundleSize {
		_ = os.Remove(part)
		return downloadResultFailed, fmt.Errorf("rule: body too large from %q: >%d bytes", url, maxBundleSize)
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return downloadResultFailed, fmt.Errorf("rule: rename %q: %w", dst, err)
	}
	return downloadResultOK, nil
}

// backoffDuration returns 1s, 2s, 4s, 8s… capped at 60s.
func backoffDuration(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 60*time.Second {
		return 60 * time.Second
	}
	return d
}

