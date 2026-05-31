# Embedded Rules in Binary — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship k2 rule bundles inside the binary via `go:embed` so cold-start is instant/offline/censorship-proof, and collapse packaging into a single `all.krs.tar.gz`.

**Architecture:** k2-rules CI publishes a new `all.krs.tar.gz` (all `.krs`) alongside the unchanged `k2-rules.tar.gz` (Phase A overlap). The k2 build pipeline fetches `all.krs.tar.gz` at build time into `k2/rule/embed/all.krs.tar.gz` and embeds it. A new `rule.SeedFromEmbedIfEmpty(cacheDir)` extracts the embedded archive into the cache when it holds zero `.krs`; `engine.Start()` and `appext.PrefetchRules()` call it **before** `EnsureBundles`, so cold start gets an instant offline rule floor and `EnsureBundles` then sees a warm/fresh cache (no blocking download). `EnsureBundles`' download contract is left untouched (the CDN is now refresh-only via the background updater). No webapp changes.

> **Design note (why a separate function, not seeding inside `EnsureBundles`):** every existing `EnsureBundles` test starts from an empty dir and expects a *download*. Seeding inside `ensureOnce` would make 8 of them seed-and-return instead, forcing fragile per-test surgery. Keeping seeding in its own `SeedFromEmbedIfEmpty` (called from the engine/appext, ahead of `EnsureBundles`) leaves all 8 download tests green and gives clean floor-vs-refresh separation. This is a deliberate, improving deviation from spec §5.6's "seed inside EnsureBundles" wording.

**Tech Stack:** Go (`go:embed`, `archive/tar`, `compress/gzip`), gomobile bind, Makefile, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-05-31-embedded-rules-binary-design.md`

---

## Repo boundaries (read first)

This plan touches **three** git repos. Commit each in its own repo:

| Layer | Repo | Path | Branch |
|---|---|---|---|
| Packaging (Task 1) | `k2-rules` (standalone) | `/Users/david/projects/kaitu-io/k2-rules` | `master` |
| Embed + seed (Tasks 2–5, 7) | `k2` submodule | `/Users/david/projects/kaitu-io/k2app/k2` | `master` |
| Build-fetch (Task 6) | `k2app` (parent) | `/Users/david/projects/kaitu-io/k2app` | `main` |

> **k2 submodule rule:** CLAUDE.md forbids editing `k2/` from the parent *unless the task explicitly targets k2*. This task **does** target k2 (embed.go etc.), so edits to `k2/rule/` are in-scope. Commit them inside the submodule (`cd k2 && git commit ...`), not from the parent.

## File Structure

| File | Repo | Responsibility | Change |
|---|---|---|---|
| `.github/workflows/daily-build.yml` | k2-rules | Build + publish `all.krs.tar.gz` (Phase A) + verify | Modify |
| `rule/embed/all.krs.tar.gz` | k2 | Committed placeholder (cn baseline ~65 KB); build-fetch overwrites for release | Create |
| `rule/embed.go` | k2 | `//go:embed` the archive; `seedFromEmbed`; `SeedFromEmbedIfEmpty`; `extractTarballReader` | Create |
| `rule/embed_test.go` | k2 | Validate embedded archive; seed + compose-with-EnsureBundles tests | Create |
| `rule/ensure.go` | k2 | `tarballName` flip; refactor `extractTarball` → reader-based core | Modify |
| `rule/ensure_test.go` | k2 | `makeTarballHandler` suffix → `all.krs.tar.gz` | Modify |
| `engine/engine.go` | k2 | Call `SeedFromEmbedIfEmpty` before `EnsureBundles` in `Start()` | Modify |
| `appext/appext.go` | k2 | Call `SeedFromEmbedIfEmpty` before `EnsureBundles` in `PrefetchRules` | Modify |
| `Makefile` | k2app | `fetch-rules-embed` target wired into `pre-build` | Modify |
| `scripts/check-embed-size.sh` | k2app | Guard: committed embed blob ≤ 300 KB | Create |

---

## Task 1: k2-rules CI — publish `all.krs.tar.gz` (Phase A overlap)

**Repo:** `/Users/david/projects/kaitu-io/k2-rules` (branch `master`)

**Files:**
- Modify: `.github/workflows/daily-build.yml`

Phase A **adds** the new archive and leaves `k2-rules.tar.gz` carrying `.krs` so currently-released clients are not stranded. Phase B (stripping `.krs`) is a later one-line edit, not in this plan.

- [ ] **Step 1: Read the current build/release/sync/purge steps**

Run: `grep -nE "tar -czf|gh release create|cp dist|paths=\(" .github/workflows/daily-build.yml`
Expected: the `tar -czf k2-rules.tar.gz *.k2b *.krs` line (~42), the `gh release create` asset list (~88-94), the `cp dist/*...` sync line (~106), the jsdelivr `paths=(...)` line (~140).

- [ ] **Step 2: Add `all.krs.tar.gz` to the "Build tarball + VERSION" step**

Find:
```bash
          tar -czf k2-rules.tar.gz *.k2b *.krs
          ls -lh k2-rules.tar.gz
          printf '%s sha256:%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(sha256sum k2-rules.tar.gz | awk '{print $1}')" > VERSION
```
Replace with:
```bash
          # all.krs.tar.gz: the single .krs archive the embed/build-fetch and the
          # embed-aware client consume. Phase A: k2-rules.tar.gz UNCHANGED (still
          # carries .krs) so pre-embed clients keep their source. Phase B (later)
          # strips .krs from k2-rules.tar.gz — see embedded-rules spec §9.
          tar -czf all.krs.tar.gz *.krs
          tar -czf k2-rules.tar.gz *.k2b *.krs
          ls -lh all.krs.tar.gz k2-rules.tar.gz
          # VERSION/change-detection keys on all.krs.tar.gz (the .krs set is what
          # matters now; .k2b is frozen legacy).
          printf '%s sha256:%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(sha256sum all.krs.tar.gz | awk '{print $1}')" > VERSION
```

- [ ] **Step 3: Add a verification step (the test for this CI change)**

Insert immediately AFTER the "Build tarball + VERSION" step, BEFORE "Check for changes":
```yaml
      - name: Verify archive split
        working-directory: dist
        run: |
          set -euo pipefail
          # all.krs.tar.gz must contain every .krs and zero .k2b.
          krs_in_all=$(tar -tzf all.krs.tar.gz | grep -c '\.krs$' || true)
          k2b_in_all=$(tar -tzf all.krs.tar.gz | grep -c '\.k2b$' || true)
          echo "all.krs.tar.gz: ${krs_in_all} .krs, ${k2b_in_all} .k2b"
          [ "$krs_in_all" -ge 1 ] || { echo "FAIL: all.krs.tar.gz has no .krs"; exit 1; }
          [ "$k2b_in_all" -eq 0 ] || { echo "FAIL: all.krs.tar.gz must not contain .k2b"; exit 1; }
          # The reject feature depends on tencent-overseas.krs being in the archive.
          tar -tzf all.krs.tar.gz | grep -q '^tencent-overseas\.krs$' \
            || tar -tzf all.krs.tar.gz | grep -q '/tencent-overseas\.krs$' \
            || { echo "FAIL: tencent-overseas.krs missing from all.krs.tar.gz"; exit 1; }
          echo "OK: archive split verified"
```

- [ ] **Step 4: Add `all.krs.tar.gz` to release upload**

In the "Create release" step, find the `gh release create` asset list (the lines listing `dist/k2-rules.tar.gz`, `dist/VERSION`, etc.) and add `dist/all.krs.tar.gz` to it. Concretely, find:
```bash
            dist/k2-rules.tar.gz \
```
Replace with:
```bash
            dist/all.krs.tar.gz \
            dist/k2-rules.tar.gz \
```

- [ ] **Step 5: Add `all.krs.tar.gz` to the release-branch sync**

Find:
```bash
          cp dist/*.k2b dist/*.krs dist/manifest.json dist/k2-rules.tar.gz dist/VERSION /tmp/release-artifacts/
```
Replace with:
```bash
          cp dist/*.k2b dist/*.krs dist/manifest.json dist/all.krs.tar.gz dist/k2-rules.tar.gz dist/VERSION /tmp/release-artifacts/
```

- [ ] **Step 6: Add `all.krs.tar.gz` to the jsdelivr purge**

Find:
```bash
          paths=(k2-rules.tar.gz VERSION manifest.json)
```
Replace with:
```bash
          paths=(all.krs.tar.gz k2-rules.tar.gz VERSION manifest.json)
```

- [ ] **Step 7: Lint the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/daily-build.yml')); print('yaml ok')"`
Expected: `yaml ok`

- [ ] **Step 8: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2-rules
git add .github/workflows/daily-build.yml
git commit -m "build(ci): publish all.krs.tar.gz alongside k2-rules.tar.gz (Phase A)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: k2 — committed placeholder + `go:embed`

**Repo:** `/Users/david/projects/kaitu-io/k2app/k2` (branch `master`)

**Files:**
- Create: `rule/embed/all.krs.tar.gz` (placeholder — cn baseline)
- Create: `rule/embed.go`
- Create: `rule/embed_test.go`

The placeholder must exist (and be a *valid* tarball with ≥1 `.krs`) so `go:embed` compiles and `seedFromEmbed` tests have real bundles. It stays small (cn baseline) and is essentially never updated; `fetch-rules-embed` (Task 6) overwrites it for release builds.

- [ ] **Step 1: Build the placeholder from the current production archive**

Run:
```bash
cd /Users/david/projects/kaitu-io/k2app/k2
mkdir -p rule/embed
tmp=$(mktemp -d)
curl -fsSL "https://github.com/kaitu-io/k2-rules/releases/latest/download/all.krs.tar.gz" -o "$tmp/all.krs.tar.gz" 2>/dev/null \
  || curl -fsSL "https://cdn.jsdelivr.net/gh/kaitu-io/k2-rules@release/all.krs.tar.gz" -o "$tmp/all.krs.tar.gz"
mkdir -p "$tmp/x" && tar -xzf "$tmp/all.krs.tar.gz" -C "$tmp/x"
# Placeholder = cn baseline (cn + tencent-overseas), ~65 KB. Just a compile/test
# fixture; the release build-fetch (Task 6) overwrites it with the full archive.
# Deliberately omit overseas.krs (~500 KB) to keep well under the 300 KB guard.
tar -czf rule/embed/all.krs.tar.gz -C "$tmp/x" cn.krs tencent-overseas.krs
ls -l rule/embed/all.krs.tar.gz
rm -rf "$tmp"
```
Expected: `rule/embed/all.krs.tar.gz` exists, ~65 KB (well under the 300 KB guard).

> Note: if `all.krs.tar.gz` is not yet published when running this (Task 1 not merged/built), fetch `k2-rules.tar.gz` instead (it still contains the same `.krs`) and extract the three files from it.

- [ ] **Step 2: Write the embed file**

Create `rule/embed.go`:
```go
package rule

import _ "embed"

// embeddedRules is the gzipped tarball of all .krs rule bundles, baked into
// the binary at build time. The build pipeline (`make fetch-rules-embed`)
// overwrites rule/embed/all.krs.tar.gz with the full production archive before
// a release build; the committed copy is a small cn-baseline placeholder so the
// package always compiles and dev/test builds have real bundles.
//
// seedFromEmbed (ensure.go) extracts this into the cache on true cold start —
// instant, offline, censorship-proof. The CDN remains the source of newer
// rules via the background updater.
//
//go:embed embed/all.krs.tar.gz
var embeddedRules []byte
```

- [ ] **Step 3: Write the failing test**

Create `rule/embed_test.go`:
```go
package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestEmbeddedRules_IsUsableTarball(t *testing.T) {
	if len(embeddedRules) == 0 {
		t.Fatal("embeddedRules is empty — placeholder missing or build-fetch wrote nothing")
	}
	gz, err := gzip.NewReader(bytes.NewReader(embeddedRules))
	if err != nil {
		t.Fatalf("embeddedRules is not gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	krs := 0
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names = append(names, hdr.Name)
		if strings.HasSuffix(hdr.Name, ".krs") {
			krs++
		}
	}
	if krs == 0 {
		t.Fatalf("embedded archive has 0 .krs (names=%v)", names)
	}
	// The cn baseline (and full archive) must carry cn.krs.
	found := false
	for _, n := range names {
		if n == "cn.krs" || strings.HasSuffix(n, "/cn.krs") {
			found = true
		}
	}
	if !found {
		t.Errorf("embedded archive missing cn.krs (names=%v)", names)
	}
}
```

- [ ] **Step 4: Run the test (it should already pass — placeholder + embed are in place)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/ -run TestEmbeddedRules_IsUsableTarball -v`
Expected: PASS. (This is a guard test, not red-green — it protects against a broken placeholder/fetch.)

- [ ] **Step 5: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app/k2
git add rule/embed/all.krs.tar.gz rule/embed.go rule/embed_test.go
git commit -m "feat(rule): embed cn-baseline rule archive via go:embed

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: k2 — `extractTarballReader` refactor + `seedFromEmbed` + `SeedFromEmbedIfEmpty`

**Repo:** `/Users/david/projects/kaitu-io/k2app/k2` (branch `master`)

**Files:**
- Modify: `rule/ensure.go` (refactor `extractTarball`; add `seedFromEmbed`)
- Modify: `rule/embed.go` (add public `SeedFromEmbedIfEmpty`)
- Modify: `rule/embed_test.go` (add seed + no-op tests)

`seedFromEmbed` does the extraction (lock-agnostic). `SeedFromEmbedIfEmpty` is the public entry point the engine/appext call: fast no-op when warm, flock-guarded seed when cold.

- [ ] **Step 1: Write the failing tests**

Add to `rule/embed_test.go`:
```go
func TestSeedFromEmbedIfEmpty_SeedsColdCache(t *testing.T) {
	dir := t.TempDir()

	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("SeedFromEmbedIfEmpty: %v", err)
	}
	if countKRS(dir) == 0 {
		t.Fatal("cold cache not seeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); err != nil {
		t.Errorf("cn.krs not seeded: %v", err)
	}
	v, err := os.ReadFile(filepath.Join(dir, "bundles.version"))
	if err != nil {
		t.Fatalf("bundles.version not written: %v", err)
	}
	if !strings.Contains(string(v), "embedded") {
		t.Errorf("bundles.version missing 'embedded' marker: %q", v)
	}
	if !isVersionFresh(dir, bundlesFreshTTL) {
		t.Error("cache not fresh after seeding")
	}
	if _, err := os.Stat(filepath.Join(dir, ".seed-tmp")); !os.IsNotExist(err) {
		t.Errorf(".seed-tmp should be removed, stat err=%v", err)
	}
}

func TestSeedFromEmbedIfEmpty_NoOpWhenWarm(t *testing.T) {
	dir := t.TempDir()
	// A cache that already has a .krs must NOT be re-seeded or mutated.
	marker := filepath.Join(dir, "existing.krs")
	if err := os.WriteFile(marker, []byte{0x4b, 0x32, 0x42, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("SeedFromEmbedIfEmpty on warm cache: %v", err)
	}
	// No embed seed happened → cn.krs must NOT appear, existing.krs untouched.
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); !os.IsNotExist(err) {
		t.Errorf("warm cache was re-seeded (cn.krs appeared), stat err=%v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing.krs disturbed: %v", err)
	}
}

func TestSeedFromEmbedIfEmpty_EmptyCacheDirIsNoOp(t *testing.T) {
	if err := SeedFromEmbedIfEmpty(""); err != nil {
		t.Errorf("empty cacheDir should be a no-op, got %v", err)
	}
}
```
Add `"os"` and `"path/filepath"` to `embed_test.go` imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/ -run TestSeedFromEmbedIfEmpty -v`
Expected: FAIL — compile error `undefined: SeedFromEmbedIfEmpty`.

- [ ] **Step 3: Refactor `extractTarball` to accept an `io.Reader`**

In `rule/ensure.go`, replace the function head:
```go
func extractTarball(tarballPath, newDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
```
with a thin wrapper plus a reader-based core:
```go
func extractTarball(tarballPath, newDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarballReader(f, newDir)
}

// extractTarballReader unpacks .krs and .yaml entries from a gzipped tar stream
// into newDir, stripping directory components (path-traversal defense). Other
// entry types and non-regular files are ignored. Shared by CDN downloads
// (extractTarball) and the binary-embedded seed (seedFromEmbed).
func extractTarballReader(r io.Reader, newDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
```
Leave the rest of the loop body (`for { hdr, err := tr.Next() ... }`) exactly as-is — it already lives below this point and now belongs to `extractTarballReader`.

- [ ] **Step 4: Add `seedFromEmbed` (lock-agnostic extractor)**

Add to `rule/ensure.go` (after `extractTarballReader`):
```go
// seedFromEmbed extracts the binary-embedded all.krs.tar.gz into cacheDir.
// Extracts to a temp dir then atomicSwap so a concurrent reader never sees a
// half-written cache. Writes bundles.version stamped "embedded" so the cache
// reads fresh. Lock-agnostic: the public SeedFromEmbedIfEmpty wraps it in the
// cache flock.
func seedFromEmbed(cacheDir string) error {
	tmpDir := filepath.Join(cacheDir, ".seed-tmp")
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarballReader(bytes.NewReader(embeddedRules), tmpDir); err != nil {
		return fmt.Errorf("rule: extract embedded: %w", err)
	}
	if countKRS(tmpDir) == 0 {
		return errors.New("rule: embedded archive contained 0 .krs (broken placeholder or build-fetch)")
	}
	if err := atomicSwap(cacheDir, tmpDir); err != nil {
		return fmt.Errorf("rule: seed atomic swap: %w", err)
	}

	sum := sha256.Sum256(embeddedRules)
	stamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s embedded sha256:%s\n", stamp, hex.EncodeToString(sum[:]))
	return os.WriteFile(filepath.Join(cacheDir, "bundles.version"), []byte(line), 0o644)
}
```
Add `"bytes"` to the `rule/ensure.go` import block (the others — `context`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `io`, `os`, `path/filepath`, `time` — are already imported).

- [ ] **Step 5: Add the public `SeedFromEmbedIfEmpty` to `rule/embed.go`**

Append to `rule/embed.go`:
```go
import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// SeedFromEmbedIfEmpty seeds cacheDir from the embedded archive when it holds
// zero .krs (true cold start); no-op otherwise. Offline and instant. Safe
// across processes (cache flock) and goroutines. Call BEFORE EnsureBundles so
// a cold start gets a working rule floor without waiting on the network — the
// subsequent EnsureBundles then sees a fresh cache and skips the download.
func SeedFromEmbedIfEmpty(cacheDir string) error {
	if cacheDir == "" {
		return nil
	}
	if countKRS(cacheDir) > 0 {
		return nil // warm — fast path, no lock
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := acquireLock(ctx, filepath.Join(cacheDir, ".ensure.lock"))
	if err != nil {
		return err
	}
	defer release()
	if countKRS(cacheDir) > 0 {
		return nil // another process/goroutine seeded while we waited
	}
	if err := seedFromEmbed(cacheDir); err != nil {
		return err
	}
	slog.Info("rule: seeded cache from embedded archive", "dir", cacheDir)
	return nil
}
```
> `embed.go` already has `import _ "embed"` from Task 2. Merge that single import into this grouped `import (...)` block (keep the blank `_ "embed"` line) so the file has one import block.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/ -run 'TestSeedFromEmbedIfEmpty|TestEmbeddedRules' -v`
Expected: PASS, no warnings.

- [ ] **Step 7: Run the full rule package — all 8 existing download tests must still pass (EnsureBundles is untouched)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/`
Expected: `ok` — including `TestEnsureBundles_ColdDownload`, `TestEnsureBundles_AllSourcesFail`, etc. (they never invoke the seed).

- [ ] **Step 8: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app/k2
git add rule/ensure.go rule/embed.go rule/embed_test.go
git commit -m "feat(rule): SeedFromEmbedIfEmpty — offline cold-start rule floor from embed

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: k2 — wire `SeedFromEmbedIfEmpty` into `engine.Start()` + `appext.PrefetchRules`

**Repo:** `/Users/david/projects/kaitu-io/k2app/k2` (branch `master`)

**Files:**
- Modify: `rule/embed_test.go` (composition test — proves the engine's seed→ensure sequence works offline)
- Modify: `engine/engine.go` (`Start()` — call seed before `EnsureBundles`)
- Modify: `appext/appext.go` (`PrefetchRules` — call seed before `EnsureBundles`)

The composition (seed-then-ensure, offline) is unit-tested at the rule level; the two engine/appext call-sites are 1-liners verified by `go build`/existing tests and the offline device smoke (Final Verification).

- [ ] **Step 1: Write the failing composition test**

Add to `rule/embed_test.go`:
```go
func TestColdStartComposition_SeedThenEnsureOffline(t *testing.T) {
	dir := t.TempDir()
	// Mirror engine.Start(): seed the embed floor, then EnsureBundles with an
	// unreachable source. Cold start must succeed offline and leave a usable
	// cache — EnsureBundles sees the fresh seeded cache and skips the network.
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"}); err != nil {
		t.Fatalf("EnsureBundles after seed should be a fresh no-op, got %v", err)
	}
	if countKRS(dir) == 0 {
		t.Fatal("composition left no .krs")
	}
}
```
Add `"context"` and `"time"` to `embed_test.go` imports (if not already present from earlier tasks).

- [ ] **Step 2: Run the test — it should already PASS**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/ -run TestColdStartComposition_SeedThenEnsureOffline -v`
Expected: PASS (Task 3 already built `SeedFromEmbedIfEmpty`). This test locks the contract the engine relies on; if it fails, the seed/ensure interaction is broken — fix before wiring.

- [ ] **Step 3: Wire the seed into `engine.Start()`**

In `engine/engine.go`, find the `else` branch that runs `EnsureBundles` (the block after the `client.CacheDir == ""` warning, ~line 200-203):
```go
	} else {
		ensureCtx, cancelEnsure := context.WithTimeout(ctx, 10*time.Second)
		ensureErr := rule.EnsureBundles(ensureCtx, client.CacheDir, sources)
		cancelEnsure()
```
Insert the seed at the top of the `else` block, before `ensureCtx`:
```go
	} else {
		// Seed the embedded rule floor before any network — instant, offline,
		// censorship-proof. After this the cache is warm, so EnsureBundles
		// short-circuits (fresh) instead of blocking on a cold download.
		if err := rule.SeedFromEmbedIfEmpty(client.CacheDir); err != nil {
			slog.Warn("engine: seed from embed failed", "err", err, "cacheDir", client.CacheDir)
		}
		ensureCtx, cancelEnsure := context.WithTimeout(ctx, 10*time.Second)
		ensureErr := rule.EnsureBundles(ensureCtx, client.CacheDir, sources)
		cancelEnsure()
```

- [ ] **Step 4: Wire the seed into `appext.PrefetchRules`**

In `appext/appext.go`, find the goroutine body in `PrefetchRules` (~line 311-314):
```go
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
```
Insert the seed as the first statement in the goroutine:
```go
	safego.Go(func() {
		if err := rule.SeedFromEmbedIfEmpty(dir); err != nil {
			slog.Warn("appext: seed from embed failed", "err", err, "dir", dir)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
```

- [ ] **Step 5: Build + run the affected packages**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go build ./... && go test ./rule/ ./engine/ ./appext/`
Expected: build clean; `ok ./rule/`, `ok ./engine/`, `ok ./appext/`. (`slog` is already imported in both `engine.go` and `appext.go`; if `go build` reports an unused/missing import, fix it.)

- [ ] **Step 6: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app/k2
git add rule/embed_test.go engine/engine.go appext/appext.go
git commit -m "feat(engine,appext): seed embedded rule floor before EnsureBundles

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: k2 — flip `tarballName` to `all.krs.tar.gz`

**Repo:** `/Users/david/projects/kaitu-io/k2app/k2` (branch `master`)

**Files:**
- Modify: `rule/ensure.go` (`tarballName` const)
- Modify: `rule/ensure_test.go` (`makeTarballHandler` suffix)

The CDN download is the refresh path; it must target the new archive. Because seeding lives in `SeedFromEmbedIfEmpty` (NOT in `EnsureBundles`), the existing download tests need no behavioural surgery — only the served filename changes.

- [ ] **Step 1: Update `makeTarballHandler` to serve the new filename**

In `rule/ensure_test.go`, find:
```go
		if !strings.HasSuffix(r.URL.Path, "k2-rules.tar.gz") {
```
Replace with:
```go
		if !strings.HasSuffix(r.URL.Path, "all.krs.tar.gz") {
```

- [ ] **Step 2: Grep for any other `k2-rules.tar.gz` literal in the rule package**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && grep -rn "k2-rules.tar.gz" rule/`
Expected: the `DefaultSources` doc comment in `downloader.go` (prose — leave it) and the one `makeTarballHandler` suffix just changed. Update any *other* non-comment test literal to `all.krs.tar.gz`.

- [ ] **Step 3: Flip the constant**

In `rule/ensure.go`, find:
```go
	tarballName    = "k2-rules.tar.gz"
```
Replace with:
```go
	tarballName    = "all.krs.tar.gz"
```

- [ ] **Step 4: Run the full rule package — all download tests pass with the new name**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/`
Expected: `ok`. (`raceDownload` now requests `…/all.krs.tar.gz`; the updated `makeTarballHandler` matches it. `TestEnsureBundles_ColdDownload` and the rest are unchanged and green.)

- [ ] **Step 5: Run the whole k2 module to catch cross-package fallout**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go build ./... && go test ./rule/ ./engine/ ./appext/`
Expected: build clean; `ok` for all three.

- [ ] **Step 6: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app/k2
git add rule/ensure.go rule/ensure_test.go
git commit -m "feat(rule): client downloads all.krs.tar.gz (was k2-rules.tar.gz)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: k2app — `fetch-rules-embed` build-fetch target

**Repo:** `/Users/david/projects/kaitu-io/k2app` (branch `main`)

**Files:**
- Modify: `Makefile`

Fetch the full `all.krs.tar.gz` into the k2 embed path before any k2 build, with a local-cache fallback so a transient CDN miss never fails the build.

- [ ] **Step 1: Inspect the current `pre-build` target and the k2 build steps**

Run: `grep -nE "^pre-build:|^appext-ios:|^appext-android:|gomobile bind|go build .* ./cmd/k2" Makefile`
Expected: `pre-build:` target line, the `appext-ios`/`appext-android` targets, and the Linux `go build ... ./cmd/k2` line (~131).

- [ ] **Step 2: Add the `fetch-rules-embed` target**

Add near the other build helpers in `Makefile` (top-level, not inside a recipe):
```makefile
# Embed the full production rule archive into the k2 binary at build time.
# go:embed needs k2/rule/embed/all.krs.tar.gz present; the committed copy is a
# small cn-baseline placeholder, overwritten here with the full ~2MB archive.
# Local-cache fallback: a transient CDN miss reuses whatever is already there
# (last fetch, or the placeholder) — never fails the build. Do NOT commit the
# overwritten 2MB file (scripts/check-embed-size.sh guards this).
RULES_EMBED_PATH := k2/rule/embed/all.krs.tar.gz
RULES_EMBED_URL  := https://github.com/kaitu-io/k2-rules/releases/latest/download/all.krs.tar.gz
RULES_EMBED_URL_FALLBACK := https://cdn.jsdelivr.net/gh/kaitu-io/k2-rules@release/all.krs.tar.gz

.PHONY: fetch-rules-embed
fetch-rules-embed:
	@mkdir -p $(dir $(RULES_EMBED_PATH))
	@if curl -fsSL "$(RULES_EMBED_URL)" -o "$(RULES_EMBED_PATH).tmp" && [ -s "$(RULES_EMBED_PATH).tmp" ]; then \
		mv "$(RULES_EMBED_PATH).tmp" "$(RULES_EMBED_PATH)"; \
		echo "fetch-rules-embed: refreshed ($$(wc -c < $(RULES_EMBED_PATH)) bytes) from origin"; \
	elif curl -fsSL "$(RULES_EMBED_URL_FALLBACK)" -o "$(RULES_EMBED_PATH).tmp" && [ -s "$(RULES_EMBED_PATH).tmp" ]; then \
		mv "$(RULES_EMBED_PATH).tmp" "$(RULES_EMBED_PATH)"; \
		echo "fetch-rules-embed: refreshed ($$(wc -c < $(RULES_EMBED_PATH)) bytes) from jsdelivr"; \
	elif [ -f "$(RULES_EMBED_PATH)" ]; then \
		echo "fetch-rules-embed: CDN unreachable — using existing $(RULES_EMBED_PATH)"; \
	else \
		echo "fetch-rules-embed: CDN unreachable AND no embed present — build will fail go:embed"; \
		exit 1; \
	fi
	@rm -f "$(RULES_EMBED_PATH).tmp"
```

- [ ] **Step 3: Make `pre-build` depend on `fetch-rules-embed`**

Find the `pre-build:` target line, e.g.:
```makefile
pre-build:
```
Replace with:
```makefile
pre-build: fetch-rules-embed
```
> If `pre-build` already lists prerequisites (e.g. `pre-build: something`), append: `pre-build: something fetch-rules-embed`.

- [ ] **Step 4: Cover the Linux `cmd/k2` build (it does not go through `pre-build`)**

Find the Linux build target header (the recipe containing `go build ... ./cmd/k2`, around line 125-134) and add `fetch-rules-embed` to its prerequisites. For example if it reads:
```makefile
build-linux:
```
Replace with:
```makefile
build-linux: fetch-rules-embed
```
> Match the actual target name from Step 1's grep. If desktop (Tauri) builds the k2 binary via a separate target that does not chain through `pre-build`, add `fetch-rules-embed` there too.

- [ ] **Step 5: Verify the target runs and writes the file**

Run: `cd /Users/david/projects/kaitu-io/k2app && make fetch-rules-embed && ls -l k2/rule/embed/all.krs.tar.gz`
Expected: prints `refreshed (~2000000 bytes) from origin` (or jsdelivr) and the file is ~2 MB.

- [ ] **Step 6: Restore the committed placeholder (do NOT leave the 2MB file staged)**

Run:
```bash
cd /Users/david/projects/kaitu-io/k2app
git -C k2 checkout -- rule/embed/all.krs.tar.gz
ls -l k2/rule/embed/all.krs.tar.gz
```
Expected: the file is back to the small placeholder size (~65 KB). (This proves the placeholder is the committed copy and the fetched 2 MB is transient.)

- [ ] **Step 7: Commit (Makefile only)**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add Makefile
git diff --cached --name-only   # MUST show only Makefile
git commit -m "build(mobile): fetch-rules-embed embeds all.krs.tar.gz into k2 binary

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: size guard — committed embed blob ≤ 300 KB

**Repo:** `/Users/david/projects/kaitu-io/k2app` (branch `main`)

**Files:**
- Create: `scripts/check-embed-size.sh`

A `go test` cannot guard this (the working tree holds the 2 MB fetched archive during release builds). Guard the **git-committed** blob size instead.

- [ ] **Step 1: Write the guard script**

Create `scripts/check-embed-size.sh`:
```bash
#!/usr/bin/env bash
# Fail if the COMMITTED k2 embed archive exceeds the placeholder budget.
# The build-fetched full archive (~2MB) is transient and must never be committed;
# the tracked copy is the small cn-baseline placeholder. Checks HEAD's blob size,
# immune to a working-tree overwrite from `make fetch-rules-embed`.
set -euo pipefail
MAX=307200   # 300 KB
PATH_IN_K2="rule/embed/all.krs.tar.gz"

size=$(git -C k2 cat-file -s "HEAD:${PATH_IN_K2}" 2>/dev/null || echo "missing")
if [ "$size" = "missing" ]; then
  echo "check-embed-size: ${PATH_IN_K2} not committed in k2 HEAD" >&2
  exit 1
fi
if [ "$size" -gt "$MAX" ]; then
  echo "check-embed-size: FAIL committed embed is ${size} bytes (> ${MAX})." >&2
  echo "  The 2MB build-fetched archive was committed by mistake. Restore the" >&2
  echo "  placeholder: git -C k2 checkout -- ${PATH_IN_K2}" >&2
  exit 1
fi
echo "check-embed-size: OK (${size} bytes ≤ ${MAX})"
```

- [ ] **Step 2: Make it executable and run it**

Run:
```bash
cd /Users/david/projects/kaitu-io/k2app
chmod +x scripts/check-embed-size.sh
scripts/check-embed-size.sh
```
Expected: `check-embed-size: OK (<placeholder size> bytes ≤ 307200)`.

- [ ] **Step 3: Negative check — prove the guard fails on a 2 MB blob**

Run:
```bash
cd /Users/david/projects/kaitu-io/k2app
make fetch-rules-embed >/dev/null 2>&1 || true   # working tree now 2MB
git -C k2 add rule/embed/all.krs.tar.gz           # stage the big file
git -C k2 stash push -- rule/embed/all.krs.tar.gz >/dev/null 2>&1 || true
# Guard checks HEAD (still the placeholder) — should pass even with a dirty tree:
scripts/check-embed-size.sh
git -C k2 stash drop >/dev/null 2>&1 || true
git -C k2 checkout -- rule/embed/all.krs.tar.gz
```
Expected: still `OK` — confirming the guard reads HEAD, not the dirty working tree. (To see it FAIL you would have to actually commit the 2 MB blob; the guard exists to catch exactly that in CI/pre-commit.)

- [ ] **Step 4: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add scripts/check-embed-size.sh
git commit -m "build: guard committed k2 embed blob ≤ 300KB

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification (after all tasks)

- [ ] **k2 unit tests green**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go build ./... && go test ./rule/ ./engine/ ./appext/`
Expected: build clean; `ok ./rule/`, `ok ./engine/`, `ok ./appext/`.

- [ ] **Embed seeding works offline (unit-covered)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test ./rule/ -run 'TestSeedFromEmbedIfEmpty|TestColdStartComposition' -v`
Expected: PASS — the offline seed and the engine's seed→ensure composition are both covered; no manual step.

- [ ] **Size guard passes on committed tree**

Run: `cd /Users/david/projects/kaitu-io/k2app && scripts/check-embed-size.sh`
Expected: `OK`.

- [ ] **Real-device smoke (the ship gate — NOT optional)**

Per spec §9: build a mobile app (`make dev-ios` / `make dev-android`), wipe the device App-Group `rules` cache, put the device in airplane mode, connect in cn-bypass. Verify routing is correct from the **embedded** rules with no network: `cn.krs` direct hits (`outbound(0)`) and `tencent-overseas` reject drops on `43.159.*`/`43.153.*`. If the cache seeds and routing is correct offline → embed works.

---

## Deploy Order (per spec §9)

1. **Task 1** merged + a k2-rules release built → `all.krs.tar.gz` live on Release + jsDelivr (verify the purge ran).
2. **Tasks 2–5, 7** (k2) committed; **Task 6** (k2app Makefile) committed.
3. Build mobile with `make fetch-rules-embed` in the chain → binary embeds the full archive (binary grows ~2 MB).
4. Airplane-mode cold-start smoke (Final Verification).
5. Ship. **Phase B** (strip `.krs` from `k2-rules.tar.gz`) is deferred to the `.k2b` sunset (2026-12-01) — not part of this plan.

## Out of Scope (per spec §10)

- Warm-cache 24h update-latency (manifest/version-check incremental refresh).
- `.k2b` / `k2-rules.tar.gz` removal (Phase B).
- Region-scoped embed (binary-size trim for non-RU/IR).
- Any webapp / VPN-state-machine change.
