# tmp pin cache & bundle lifecycle — full notes

Moved **verbatim** from `rule/CLAUDE.md` (2026-11-14) to keep that always-loaded file short. The invariants stay summarised in `rule/CLAUDE.md`; this file is the full rationale, measurements and the tests that pin each rule. **Read this before changing `tmp_lru.go`, `SetTmpRule`, `reload`, `ensure.go`, `updater.go`, `embed/` or the named-bundle loaders.**

**Corrections vs code (2026-11-14)** — the verbatim text below predates these: (1) "the DNS pipeline re-pins on every cache hit" is wrong — `core/dns/middleware.go` step 6 returns a cache hit without pinning; pins are set on upstream resolve (step 10) and prefresh. (2) "Four write sites" — there are three (`NewEngine`, `SetTmpRule`, `Close`); `reload` no longer touches the pin structures.

## tmp Pin Cache (DNS-learned per-IP pins)

`SetTmpRule(ip, target)` records the DNS-time decision for a resolved IP so the
later IP-routed connection reaches the same outbound. It is the **only** bridge
from `Match(domain)` to `Match(ip)` when exception routes exist.

- Gated on `len(e.routes) != 0`, **not** on `DisableSniff()` — those answer
  different questions. With zero exception routes every host takes the fallback
  and a pin can never change a decision, so pinning is skipped purely to bound
  the dirty heap.
- The cache is an optimization, never a source of truth: routes still match
  correctly without it.

**Capacity: `tmpRuleCap = 4096`, and a full cache evicts exactly the
least-recently-**pinned** entry** — not the whole table (the pre-LRU behavior).

Per-entry cost figures live in the comment above `tmpRuleCap` in
`rule/engine.go` (~274 B/entry across `tmp` + `tmpOrder` + `tmpIndex`, ~1.07 MB
resident at cap, vs ~349 KB for the old bare map — ~2.1% of the iOS NE 50 MB
jetsam budget). **That comment is the single number of record**; size any cap
change off it and do not restate a differently-measured figure elsewhere.

"Least recently **pinned**", not "least recently used": `MatchConn` read hits do
**not** refresh freshness, because refreshing on read would need a write lock on
the read path. The behaviors are equivalent in practice — the DNS pipeline
re-pins on every cache hit, so a live domain is continuously touched.

### The three structures move together

`e.tmp` (addr → Target), `e.tmpOrder` (`*list.List`, Front = oldest), and
`e.tmpIndex` (addr → `*list.Element`) are **one logical unit**. Clearing or
mutating one without the others leaves stale elements whose eviction becomes a
no-op `delete` — and `e.tmp` then grows past the cap silently. Four write
sites; the three mutating ones all run under `e.mu` held for writing, the
constructor runs before the engine is reachable by any other goroutine:

1. `NewEngine` — constructs all three (no lock: nothing else can see it yet).
2. `SetTmpRule` — `e.tmp[addr] = target` + `tmpLRUTouch`, plus
   `tmpLRUEvictOldest` at cap (which deletes from all three).
3. `Close` — `e.tmp = make(...)` + `tmpLRUReset()`.

`reload` (shared by `Reload` / `ReloadWithClosers` / `ReloadNamed`) touches
**none** of the three: pins survive a hot reload (since 2026-10, #3889). A pin
is the only bridge from a domain decision to the IP the app later connects to,
and nothing re-learns a dropped pin until that domain is resolved upstream
again — a DNS **cache hit returns without pinning** (`core/dns/middleware.go`
step 6), and the app caches the answer for the TTL we hand it (min 10 min).
Clearing therefore routed every already-resolved domain by IP alone for minutes
to hours. A kept pin only delays a domain whose decision changed until its next
upstream resolve or prefresh, which overwrites it under the new rules.

Helpers live in `tmp_lru.go`; none of them take a lock of their own, by design
(`Engine.mu` is standalone and never nested inside another lock).

### DIAG

`rule-tmp-pin` (carries `evictedIP` when an add displaced an entry; duplicates suppressed — cache hits never pin), `rule-tmp-lru-desync`, `rule-tmp-reload` (`pinsKept`) — level, fields and the removed `rule-tmp-evict` are in `k2/docs/diag-events.md`, the lint-enforced registry. Don't re-list event fields here.

Eviction deliberately has no line of its own: once the cache is warm, LRU
eviction fires once per new pin, so a standalone event would be a 1:1 flood —
exactly what `rule-tmp-pin`'s duplicate suppression exists to avoid.

## Bundle Lifecycle (rule data, not matching)

- `ensure.go` — `EnsureBundles`: TTL-gated refresh (`bundlesShortTTL` fast path,
  `bundlesFreshTTL` hard backstop), cross-process serialized by `flock_*.go`,
  in-process by singleflight. **Never on the connect path** (#3889): a
  singleflight joiner waits on the leader's ctx, not its own, so a connect-time
  call with a 10s budget joined the app-start prefetch's 30s download and held
  `engine.Start` for up to 30s. Callers are background only — the platform's
  app-start `PrefetchRules` and the updater.
- `manifest.go` — parses k2-rules `dist/manifest.json` (schema v2); newest-wins
  by parsed RFC3339 time, never by string compare.
- `downloader.go` — Happy Eyeballs race across `DefaultSources` (GitHub origin +
  jsdelivr/ghproxy mirrors for CN resilience).
- `embed.go` + `embed/krs.tar.gz` — build-time embedded baseline (`//go:embed`),
  extracted on true cold start (offline, censorship-proof). The committed
  archive is a small (~66 KB) baseline placeholder. The k2app-root Makefile's
  `fetch-rules-embed` (part of `pre-build`, so every `make dev-*`/`build-*`)
  **overwrites `k2/rule/embed/krs.tar.gz`** with the full CDN archive (k2-rules
  GitHub release, jsdelivr fallback); offline it keeps the placeholder unless
  `EMBED_REQUIRE_FULL=1` (release CI), which fails instead. **Never commit the
  fetched ~2 MB file** — the labtest `k2` suite and `scripts/check-embed-size.sh`
  fail above 300 KB. `k2/Makefile` targets do not fetch it.
- `updater.go` — post-connect refresh: an immediate check, then every
  `defaultCheckInterval` (1h). Each check (`refreshAndReload`) runs
  `ensureBundles`, then hot-reloads via `ReloadNamed` **iff**
  `BundleSignature` (`signature.go`: every `.krs` name/size/mtime + manifest
  version) differs from the set the engine serves — seeded by
  `WithLoadedSignature`, taken in `engine.Start` just before the load. The
  decision is "disk differs from engine", not "this call downloaded": the
  app-start prefetch can land a new set after the engine loaded the old one,
  leaving the cache fresh and the updater with nothing to download. An empty
  signature (no `.krs` on disk) never replaces a working set.
- `loadnamed.go` (heap `*krs.Bundle`) vs `opennamed.go` (mmap `NamedDiskBundle`).
  The iOS NE path **must** use `OpenNamed`: `LoadNamed` expands the full corpus
  onto the dirty heap (~19 MB); mmap keeps resident dirty heap in the tens of KB.
  `NewEngineWithClosers` / `ReloadWithClosers` carry the munmap closers, always
  run outside the state lock.
- `globs.go` — `SplitGlobs` partitions single-`*` krs patterns into
  exact/prefix pairs; unsupported globs (leading/middle `*`) are dropped with a
  WARN.
- `classify.go` — `ClassifyInstalled` answers "which installed apps default to
  direct for region X" through the **same** `krs.MatchInstalled` codepath the
  engine uses at connect time, so UI badges and routing cannot disagree.
- `miss_reporter.go` — optional rule-miss telemetry sink. Installed via
  `SetMissReporter` (atomic pointer; hot path pays one nil-check when unset).
  `Report` **must not block** — it is called from the DNS / L4 dial hot path.
