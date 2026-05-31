# Embedded Rules in Binary — Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:writing-plans next to turn this design into a task-by-task implementation plan.

**Goal:** Ship the k2 rule bundles *inside* the binary (`go:embed`) so cold-start is instant, offline, and censorship-proof — eliminating the "first download" failure class entirely — while collapsing rule packaging into a single `all.krs.tar.gz` archive.

**Architecture:** k2-rules CI publishes a new `all.krs.tar.gz` (all `.krs`). The k2 build pipeline fetches that archive at build time and embeds it via `go:embed`. On start, if the on-disk cache holds zero `.krs`, the engine seeds the cache by extracting the embedded archive (milliseconds, no network). The existing background updater still pulls newer rules from the CDN when reachable. No blocking "prepare" state, no webapp changes.

**Tech Stack:** Go (`go:embed`, `archive/tar`), gomobile bind (iOS/Android), Makefile + GitHub Actions, k2-rules build tool.

---

## 1. Background

The tunnel needs IP rule bundles (`<cc>.krs` + `overseas.krs` + `tencent-overseas.krs`) to route correctly in cn-bypass mode. Today they are fetched at first connect from a CDN tarball (`k2-rules.tar.gz`). Two problems surfaced (ticket #2481, iOS 0.4.5):

1. **First-run fragility.** Cold-start with no cached bundles + an unreachable/blocked CDN is fatal (`ErrCodeRuleBundlesUnavailable`) — exactly the GFW/first-install scenario our users hit. It also looks like a stuck "connecting".
2. **Propagation lag.** A newly published bundle (`tencent-overseas.krs`) never reached a warm-cache device because `isVersionFresh` short-circuits the download for 24h. The reject route silently no-op'd (`43.159.235.61 → outbound(2)` instead of drop).

Embedding the rules in the binary dissolves problem #1 outright and makes every future *release* carry current-as-of-build rules (which would have shipped `tencent-overseas.krs` to every device immediately). Problem #2 — warm-cache update latency *within a release's lifetime* — is explicitly **out of scope** here (see §10).

## 2. Scope

**In scope**
- New `all.krs.tar.gz` packaging (all `.krs` in one archive); strip `.krs` from `k2-rules.tar.gz` (keep `.k2b` for legacy clients).
- Build-time fetch of `all.krs.tar.gz` into a `go:embed` path.
- Embed + seed: extract the embedded archive into the cache when the cache holds zero `.krs`.
- Demote the CDN download from "fatal on cold start" to "non-fatal background refresh".

**Out of scope**
- Warm-cache 24h update-latency (manifest/version-check incremental refresh). Separate task.
- Removing `.k2b` / `k2-rules.tar.gz` (kept during the transition window; sunset date below).
- Any webapp / VPN-state-machine changes. The original "prepare" state is **dropped** — embed makes it moot.

## 3. Decisions (locked during brainstorming)

| Decision | Choice |
|---|---|
| Packaging | `all.krs.tar.gz` (all `.krs`) **+** `k2-rules.tar.gz` (`.k2b`, legacy). Two archives in parallel. End state: `k2-rules.tar.gz` is `.k2b`-only; reached via the overlap rollout in §9 so currently-released clients are never stranded. |
| Embed source | **build-fetch** — Makefile/CI pulls latest `all.krs.tar.gz` before build, places it at the `go:embed` path. No churning blob in git. |
| Embed scope | **Full** `all.krs.tar.gz` (~2 MB compressed). Every binary is region-self-sufficient. |
| Prepare state / webapp UX | **Dropped entirely.** Background refresh stays silent (as today's `StartUpdater`). |
| `.k2b` sunset | Keep building/publishing `.k2b` until **2026-12-01**, then drop `k2-rules.tar.gz` and the `.k2b` path. Revisit before the date against released-client telemetry. |

## 4. Data Flow

```
k2-rules CI ──► all.krs.tar.gz  ──► CDN (GitHub Release + jsDelivr @release)
                     │
        build-fetch  │  (make/CI, before gomobile bind / go build)
                     ▼
   k2/rule/embed/all.krs.tar.gz ──//go:embed──► k2 binary
                     │
   Engine.Start():  countKRS(cacheDir) == 0 ?
        ├─ yes ─► seed: extract embedded archive into cacheDir   (ms, offline)
        └─ no  ─► use existing cache
                     │
   StartUpdater (unchanged): when not fresh AND a mirror is reachable,
        download newer all.krs.tar.gz and hot-reload. Failure is a no-op
        (we always have at least the embedded floor).
```

**Floor/ceiling model:** the embedded archive is the *floor* (always available, build-time-current). The CDN is the *ceiling* (newer rules when reachable). Seeding lifts an empty cache to the floor; the background updater lifts it toward the ceiling over time.

## 5. Component Design

### 5.1 k2-rules packaging — `k2-rules/main.go`
No change to bundle *generation* (the `writeKRSBundle` calls for `cn.krs`, `tencent-overseas.krs`, `overseas.krs`, `<cc>.krs` stay exactly as they are). The split happens in CI tarball assembly (§5.2), not in the generator. `main.go` is untouched.

### 5.2 k2-rules CI — `k2-rules/.github/workflows/daily-build.yml`
Current "Build tarball" step (line 42):
```bash
tar -czf k2-rules.tar.gz *.k2b *.krs
```
**Phase A** (this task) — add the new archive, leave the old one untouched so released clients keep their `.krs` source:
```bash
tar -czf all.krs.tar.gz *.krs          # NEW: all .krs, ~2 MB — the client/embed source
tar -czf k2-rules.tar.gz *.k2b *.krs   # UNCHANGED during overlap (still carries .krs)
```
**Phase B** (later, post-floor — see §9) — strip `.krs` from the legacy archive:
```bash
tar -czf k2-rules.tar.gz *.k2b         # .k2b only
```
- **Change detection** keys on `all.krs.tar.gz` content (the `.krs` set is what matters now; `.k2b` is frozen legacy). Update the `VERSION` sha + the "Check for changes" comparison to use `all.krs.tar.gz`.
- **Release create** (line 94): add `dist/all.krs.tar.gz` to the uploaded assets.
- **Sync release branch** (line 106): add `all.krs.tar.gz` to the copied artifacts.
- **jsDelivr purge** (line 140): add `all.krs.tar.gz` to `paths`.
- `manifest.json` generation is unchanged (it scans `.krs` in `dist/`, independent of which tarball carries them).

### 5.3 Build-fetch — `Makefile` (+ CI)
New target, e.g. `fetch-rules-embed`, made a prerequisite of `pre-build` (which already gates `build-ios/build-android/dev-ios/dev-android`) and of the Linux `cmd/k2` and desktop k2 builds:

```makefile
RULES_EMBED_PATH := k2/rule/embed/all.krs.tar.gz
RULES_EMBED_URL  := https://github.com/kaitu-io/k2-rules/releases/latest/download/all.krs.tar.gz

fetch-rules-embed:
	@mkdir -p $(dir $(RULES_EMBED_PATH))
	@if curl -fsSL "$(RULES_EMBED_URL)" -o "$(RULES_EMBED_PATH).tmp" && [ -s "$(RULES_EMBED_PATH).tmp" ]; then \
		mv "$(RULES_EMBED_PATH).tmp" "$(RULES_EMBED_PATH)"; \
		echo "fetch-rules-embed: refreshed $(RULES_EMBED_PATH) ($$(wc -c < $(RULES_EMBED_PATH)) bytes)"; \
	elif [ -f "$(RULES_EMBED_PATH)" ]; then \
		echo "fetch-rules-embed: CDN unreachable, using existing $(RULES_EMBED_PATH)"; \
	else \
		echo "fetch-rules-embed: CDN unreachable AND no local copy — using committed placeholder"; \
	fi
	@rm -f "$(RULES_EMBED_PATH).tmp"
```
- Must run **before** `gomobile bind ./appext/` and `go build ./cmd/k2` so the embedded file is current.
- Local-cache fallback: if the CDN is unreachable at build time, reuse whatever is already at the path (last fetched, or the committed placeholder). Never fail the build on a transient CDN miss.
- Mirror list mirrors `rule.DefaultSources`; start with the GitHub Release URL, optionally try jsDelivr `@release` on failure.

### 5.4 Embed + seed — `k2/rule/embed.go` (NEW)
```go
package rule

import _ "embed"

//go:embed embed/all.krs.tar.gz
var embeddedRules []byte
```
New seed function, exercised from `EnsureBundles` when the cache is empty:
```go
// seedFromEmbed extracts the binary-embedded all.krs.tar.gz into cacheDir.
// Used when countKRS(cacheDir)==0 (true cold start). Reuses the same
// extract logic as a CDN download (keep .krs/.yaml, strip dirs), then writes
// bundles.version stamped as embedded. Offline, no network.
func seedFromEmbed(cacheDir string) error { ... }
```
- Reuse the existing `extractTarball` path by reading from `bytes.NewReader(embeddedRules)` instead of a file (refactor `extractTarball` to accept an `io.Reader`, or add `extractTarballReader`).
- After extraction, write `bundles.version` = `"<now RFC3339> embedded sha256:<sha-of-embeddedRules>"`. This marks the cache fresh; the background updater follows the existing 24h TTL. The embedded rules are build-time-current; post-build freshness is the out-of-scope §10 concern.
- The placeholder (§5.5) is a valid tarball, so seeding always yields ≥1 `.krs` even in a no-fetch dev/test build.

### 5.5 The `go:embed` placeholder
`go:embed` requires the file to exist at compile time, or the package won't build (`cd k2 && go test ./...` would fail). The full archive churns daily and is 2 MB — committing it would bloat k2's history and (wrongly) drive k2 commits from rule changes. Resolution:

- Commit a **one-time minimal placeholder** at `k2/rule/embed/all.krs.tar.gz`: a valid tar.gz containing just the cn baseline (`cn.krs` + `tencent-overseas.krs` + `overseas.krs`, ~65–235 KB). Committed once, essentially never updated.
- `fetch-rules-embed` overwrites it with the full 2 MB archive for release builds.
- The placeholder is **tracked** (must exist for `go:embed`); `fetch-rules-embed` overwrites it locally for release builds. To stop the 2 MB build-fetched archive from being committed by accident, add an enforceable guard: a CI/pre-commit check (and a `golden_test.go` assertion) that **`k2/rule/embed/all.krs.tar.gz` ≤ 300 KB in git** (the placeholder is ~65–235 KB; the full archive is ~2 MB, so the guard fails loudly if someone stages the fetched copy). This is enforceable, unlike a "please don't commit" note.
- Doubles as the dev/test fixture: unit tests that exercise `seedFromEmbed` get real (if minimal) bundles.

### 5.6 Client download target — `k2/rule/ensure.go` + `downloader.go`
- `tarballName`: `"k2-rules.tar.gz"` → **`"all.krs.tar.gz"`**. `DefaultSources` URL prefixes are unchanged (same Release/jsDelivr dirs; only the filename differs).
- `EnsureBundles` gains the seed step: when `countKRS(cacheDir)==0`, call `seedFromEmbed` **before** attempting any network download. After seeding, the cache is warm, so the normal freshness/TTL logic applies.
- **Demote cold-start fatal:** the `ErrCodeRuleBundlesUnavailable` path in `engine.go` (cold start + download failed + `LoadNamed` empty) becomes unreachable in practice (embed always seeds). Keep the guard as defense-in-depth but it should never fire; if it ever does, that signals a broken/empty embed (build bug), which is the right thing to surface loudly.

### 5.7 webapp / VPN state machine
**No changes.** No `prepare` state, no new bridge calls, no UX. Background refresh stays silent.

## 6. Failure Modes

| Scenario | Behavior |
|---|---|
| First run, CDN fully blocked (GFW) | Seed from embed → full routing works offline. **No brick.** Background refresh retries silently and no-ops on failure. |
| Build-time CDN miss | Build-fetch falls back to last-fetched / committed placeholder. Build never fails on a transient miss. (Placeholder = cn baseline, so a placeholder-only build still routes cn correctly; release CI should always reach the CDN.) |
| Embedded archive corrupt / empty | `seedFromEmbed` yields 0 `.krs`; the existing 0-krs guard + `ErrCodeRuleBundlesUnavailable` fire loudly. This is a build defect, caught by CI tests (§7). |
| Newer rules published after build | Device runs embedded rules until the background updater refreshes (≤24h TTL) or the user updates the app (new embed). Accepted; §10. |
| Legacy client still reading `.k2b` | Unaffected — `k2-rules.tar.gz` still ships `.k2b` until the sunset date. |

## 7. Testing Strategy (TDD)

All offline/deterministic — no network at test time.

- **k2/rule `embed_test.go`**: `embeddedRules` is non-empty and is a valid gzip tarball containing ≥1 `.krs` (guards a broken placeholder / fetch).
- **k2/rule seed**: `seedFromEmbed` into a temp dir → `countKRS > 0`, `cn.krs` present, `bundles.version` written with the `embedded` marker; `isVersionFresh` true afterward.
- **k2/rule `EnsureBundles`**: empty cache + no network → seeds from embed (no error), cache warm. Existing `TestEnsureBundles_*` still pass with `tarballName == all.krs.tar.gz`.
- **k2-rules CI**: a unit/integration check that `all.krs.tar.gz` extracts the expected `.krs` set and `k2-rules.tar.gz` contains **no** `.krs` (only `.k2b`). Reuse the existing `validateTencentOverseas` anchor pattern to assert `tencent-overseas.krs` is in `all.krs.tar.gz`.
- **Makefile**: `fetch-rules-embed` is idempotent; with CDN stubbed unreachable and a placeholder present, it leaves the placeholder intact and exits 0.

## 8. Backward Compatibility

- Released clients reading `k2-rules.tar.gz` for `.k2b`: keep working (archive still published, now `.k2b`-only).
- Released clients (≤ current) download `k2-rules.tar.gz` and extract `.krs` from it — today's shipped client has `tarballName == "k2-rules.tar.gz"`. If CI strips `.krs` from that archive immediately, those clients lose their rule source. **Therefore `k2-rules.tar.gz` must keep carrying `.krs` until the embed client (which points at `all.krs.tar.gz`) is the release floor.** The overlap rollout in §9 enforces this: Phase A keeps `.krs` in `k2-rules.tar.gz`; Phase B strips it only after the old client is below the support floor.

### Sequencing (overlap — the chosen path)
The clean split ("`k2-rules.tar.gz` = `.k2b` only") must not strand currently-released clients that pull `.krs` from `k2-rules.tar.gz`. The overlap rollout (§9) is the chosen path:
- **Phase A:** CI adds `all.krs.tar.gz` while `k2-rules.tar.gz` *still keeps* `.krs`. Ship the embed client (reads embed + downloads `all.krs.tar.gz`). Old clients keep pulling `.krs` from `k2-rules.tar.gz`.
- **Phase B:** once the pre-embed client is below the support floor, CI drops `.krs` from `k2-rules.tar.gz` (→ `.k2b`-only). Governed by the `.k2b` sunset (2026-12-01).

The plan only needs to detail the exact Phase-A CI/client changes; Phase B is a one-line CI edit scheduled later.

## 9. Rollout / Deploy Order

1. **k2-rules CI**: publish `all.krs.tar.gz` (Phase A overlap — `k2-rules.tar.gz` still has `.krs`). Verify both archives on Release + jsDelivr + purge.
2. **k2**: add `embed.go` + placeholder + `seedFromEmbed`; flip `tarballName` → `all.krs.tar.gz`; demote cold-start fatal. All `go test ./...` green.
3. **Makefile/CI**: add `fetch-rules-embed` to the build graph. Verify a release build embeds the full 2 MB archive (binary grows ~2 MB).
4. **Smoke**: cold device, airplane-mode first connect → routes correct from embed (cn-bypass: `cn.krs` direct + `tencent-overseas` reject fire) with **no network**.
5. **k2-rules CI Phase B** (after embed client is the floor): drop `.krs` from `k2-rules.tar.gz`. Governed by `.k2b` sunset (2026-12-01).

## 10. Out of Scope / Follow-ups

- **Warm-cache update latency.** Embedded rules are build-time-current; mid-release rule changes still propagate via the 24h-TTL background refresh. A manifest/version-check incremental refresh (fetch only changed `.krs`) is the structural fix — separate task. Optionally, seed could stamp `bundles.version` with an *old* mtime to force one early opportunistic refresh on first run (embed remains the floor on failure); deferred.
- **`.k2b` removal** after the sunset date.
- **Binary-size trim**: `ru.krs`+`ir.krs` are 76% of the embed; a per-build region-scoped embed could shrink non-RU/IR binaries. Deferred — full embed chosen for simplicity.
