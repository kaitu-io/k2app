# krs Disk-Backed Memory Constitution — Design Spec

**Status:** Draft for review
**Date:** 2026-06-01
**Repos touched:** `k2-rules` (library), `k2` (submodule consumer), `k2app` (docs)

## Goal

Make the krs rule subsystem hold its data on disk and access it via read-only
`mmap`, so a loaded bundle's resident **dirty heap is `O(set count)` — never
`O(rule count)`**. This eliminates the iOS Network-Extension jetsam OOM and
codifies "memory to the extreme" as an enforced constitution.

## Root Cause (confirmed 10/10)

iOS kernel, device `c97551…`, build k2 `47cc260` (v0.4.5 embed):

```
kernel  memorystatus: PacketTunnelExtension [5467] exceeded mem limit: ActiveHard 50 MB (fatal)
```

The NE process is SIGKILL'd ~200 ms after every connect (deterministic / 必现).
The regression: the krs migration (v0.4.4 had no `k2-rules` dep; `47cc260`
depends on `k2-rules v0.1.1`) plus `all.krs.tar.gz` (every region in one archive)
made `engine.buildRuleEngine` call `rule.LoadNamed` → `os.ReadFile` + full
in-heap expand of **all 19 region bundles**, retained resident for the tunnel
lifetime via `Engine.bundles`, even under `allProxy` where rules are never
consulted. Commit `63e25b1` had deleted the per-region selectivity
(`BundleByRegion`).

### Measured data (the proof)

`all.krs.tar.gz` = 19 regions, 6.0 MB decompressed. Section-index parse:

- ~250,720 domain entries, 113,117 IPv4 ranges, 26,753 IPv6 ranges.
- Every region has **1–3 named sets** (cn=2, overseas=1, ru=3, ir=3, rest=2).

Dirty-heap (what jetsam `phys_footprint` counts) per approach:

| Scenario | dirty heap | scaling |
|---|---|---|
| Current — all 19 regions full-expand (**the bug**) | **19.2 MB** | O(rules) |
| C: region-scope cn+overseas, full-expand | 1.68 MB | O(rules) |
| A: mmap + **heap** offset index (cn+os / all19) | 182 KB / 1.05 MB | O(rules) |
| **B: mmap + on-disk index (chosen)** | **~21 KB** | **O(sets)** |

19.2 MB of rule heap on a 50 MB NE budget is the OOM, fully consistent with the
kernel log and the connect+200 ms death timing.

## The Constitution

To be written verbatim to `k2-rules/krs/CONSTITUTION.md` and referenced from
`k2/rule/CLAUDE.md`:

> **krs Memory Constitution**
>
> 1. The rule corpus **and all indexes** live on disk. Runtime access is
>    exclusively via read-only `mmap` (clean / file-backed pages — excluded
>    from, or trivially reclaimable under, iOS jetsam `phys_footprint`).
> 2. **Invariant (testable): a loaded bundle's resident dirty heap MUST be
>    `O(number of sets)`, never `O(number of rules)`.** No per-rule heap object
>    is permitted — no `[]string` domain table, no `[][]byte` IP table.
> 3. Load **only** the regions actually referenced by the active config
>    (user region + `overseas`). Never load the whole corpus.
> 4. Hot-path lookups allocate **O(1) transient** only (normalize/reverse the
>    query once). Never per-set, never per-rule.
>
> Rationale: the iOS NE has a hard 50 MB `ActiveHard` jetsam ceiling. Read-only
> mmap converts rule data from counted dirty heap into reclaimable clean
> file-backed pages, the only way to fit a growing corpus in a fixed budget.

### Why mmap is jetsam-cheap (physical basis)

jetsam meters `phys_footprint` = dirty + compressed + iokit. Read-only mmap of
a file produces **clean, file-backed** pages: under pressure the kernel evicts
them and re-faults from disk, so they are not a hard footprint liability and the
Go GC never scans them. (Same technique sing-box and embedded DBs use on iOS.)

## Architecture Decision

**Chosen: B — mmap reader + on-disk index, delivered as an *additive*,
backward-compatible format extension.**

- **Rejected A** (mmap + heap-built offset index): its offset table is `O(rules)`
  dirty heap (182 KB → 1 MB). Violates invariant #2.
- **Rejected C** (region-scope only, keep full-expand): 1.68 MB dirty heap,
  still `O(rules)`. Acceptable as nothing-else mitigation, not the end state.
- **Dropped ③** (merge sets / reverse-domain trie): with ≤3 sets per region the
  `routes×sets` factor is ≤3 (not a CPU hotspot) and merging leaves entry count —
  hence index bytes — unchanged (**~0 memory benefit**). A trie/FST would shrink
  *disk* via suffix sharing, but mmap clean pages are already jetsam-cheap; the
  ROI does not justify the complexity. Out of scope.

### Why B is additive (low blast radius)

The format already silently skips unknown section TypeIDs (forward-compat). So:

- The variable-width **domain** sections gain a new companion **index section**
  (new TypeID) carrying a `u32` offset per entry. Old `ReadBundle` skips it and
  still works; the new `Open` uses it for in-place binary search on mmap.
- The **IP** sections are already fixed-width (`2 + 2·addrLen` bytes) and sorted
  by `(set_idx, start)` → **binary-searchable in place on mmap with zero index,
  zero heap**. No format change needed for IP.

`all.krs.tar.gz` grows ~4 bytes/domain (~1 MB raw, far less after gzip). No old
client breaks; no released client currently ships the v0.4.5 embed build.

## Components & Data Flow

### k2-rules (library)

```
krs.Open(path) ──mmap RO──▶ DiskBundle{ data []byte (mmap), sets []diskSet }
                              each diskSet: section descriptors (offset+count
                              into mmap) for domainSuffix / domainExclude /
                              ipv4 / ipv6 / their domain-offset-index
DiskBundle.MatchDomain(host) ─ binary search on mmap via offset-index
DiskBundle.MatchIP(addr)     ─ binary search on mmap (fixed-width, in place)
DiskBundle.Close()           ─ munmap
```

- **New unit `reader_mmap.go`**: `Open(path) (*DiskBundle, error)`, `Close()`,
  and the mmap-backed `Match*` methods. Implements the same match semantics as
  today's `domainSection.Match` / `ipRangeSection.Contains`, but reads operands
  from the mmap slice instead of heap slices.
- **New unit `mmap_*.go`** (build-tagged): minimal RO mmap returning `[]byte`
  on darwin/linux/android (`syscall.Mmap`) and windows (`MapViewOfFile`).
  Recommendation: hand-rolled ~50 LOC/platform; reconsider `x/exp/mmap` at plan
  time if its byte-at-a-time `ReaderAt` proves acceptable (it likely is not for
  binary search).
- **Writer change `writer.go`**: emit a `typeDomainSuffixIndex` /
  `typeDomainExcludeIndex` section (u32 offsets, parallel to the existing
  domain payload, same `(set_idx, value)` order). Bump informational `Version`.
- **Keep `ReadBundle`/`Load`** unchanged for tooling/tests/non-memory-constrained
  callers (the producer pipeline, golden tests). They simply ignore the new
  index sections. The constitution governs the **client runtime path** (`Open`).
- **Shared match core**: factor the suffix-walk / range-compare so the heap
  reader and the mmap reader share one algorithm with two operand sources
  (avoid two divergent implementations).

### k2 (consumer, engine)

- **Region scoping** (`engine.buildRuleEngine` / `loadRuleEngine`): compute the
  region set = union of `match.region` values referenced by `client.Routes`
  (typically `{userRegion, overseas}`); `Open` exactly those `.krs` files. Never
  `LoadNamed` the whole cache. Keep the existing `IsAllProxy() && len==1`
  fast-path (loads nothing).
- **Lifecycle**: `Engine` holds `[]*krs.DiskBundle`; `Engine.Close()` munmaps
  all on tunnel teardown. mmap survives an atomic-rename bundle update (open
  mapping keeps the old inode; next `Open` after refresh picks up the new file).
- **① IDNA hoist**: normalize host to ASCII-LDH **once** at the `Engine.Match` /
  `MatchConn` boundary; pass the normalized host (and its reversed parent
  suffixes) down. Removes the `2 × sets × routes` redundant `idna.ToASCII`.
- **② reversed parents once**: compute the host's L reversed parent suffixes
  once per lookup; reuse across all sets. Removes per-set `reverseASCII` allocs.
- **`e.tmp` discipline**: under `allProxy` do **not** pin DNS-learned IPs (never
  consulted); otherwise bound the map (cap + simple eviction or per-session
  reset) so it cannot grow unbounded.

### Docs

- `k2-rules/krs/CONSTITUTION.md` — the constitution verbatim.
- `k2/rule/CLAUDE.md` — a "Memory Constitution (krs)" section linking it, plus
  the invariant test as the enforcement mechanism.

## Error Handling

- `Open` failure (missing file, bad magic, truncated/out-of-bounds index, mmap
  syscall error) → return error; caller logs and treats that region as absent
  (existing missing-region path: fail-closed per `7933d1b`, not silent all-proxy).
- Corrupt/short domain-index section → `Open` errors for that bundle (do **not**
  silently fall back to full-expand on the NE — that would reintroduce the OOM).
- Bounds checks on every mmap read (offset+len ≤ mapped length); a hostile CDN
  bundle must not OOB-read. Re-validate sort order lazily is **not** allowed on
  the NE (it would touch/dirty the whole file); instead trust the writer +
  validate at publish time in k2-rules CI.

## Testing

- **Constitution invariant test (the guard):** load `cn` + `overseas` via `Open`,
  force GC, assert `runtime.MemStats.HeapInuse` delta `< 100 KB` (vs `Open` of an
  empty dir). This is the executable form of invariant #2 and lives in **both**
  k2-rules (`Open` unit) and k2 (engine integration).
- **Match parity:** table-driven — for a fixed bundle, `Open(...).MatchDomain/IP`
  must agree with `ReadBundle(...).MatchDomain/IP` on a domain/IP corpus
  (subdomain boundary, exclude precedence, 4-in-6, range edges).
- **Format round-trip:** `WriteBundle` (with index sections) → `ReadBundle`
  (skips index, full-expand) and → `Open` (uses index) both match; byte-exact
  writer determinism preserved.
- **Hot-path allocation:** `testing.AllocsPerRun` on `Engine.Match` ⇒ O(1)
  (a small constant), independent of set/route count.
- **Region scoping:** config referencing only `cn` Opens exactly `{cn, overseas}`
  (or `{cn}`); never the other 17.
- **Lifecycle:** `Open` → `Close` munmaps; atomic-rename swap under a live
  mapping does not crash and keeps serving old data until `Close`.
- **Cross-platform mmap:** darwin/linux/android/windows unit coverage.

## Out of Scope

- ③ set-merge / reverse-domain trie / FST (CPU-only, ~0 memory benefit at ≤3
  sets).
- On-disk index for IP (already fixed-width / in-place searchable).
- Changing the producer/source pipeline beyond emitting the new index sections.

## Risks / Open Questions

- **mmap on iOS App Group file**: read-only mmap of a file in
  `group.io.kaitu/.../rules/*.krs` from the NE — expected to work (plain file);
  verify on device during the foundation smoke.
- **Windows mmap lifetime** vs the rule updater's atomic rename: on Windows a
  mapped file may block rename; plan must confirm the updater's swap strategy on
  Windows (desktop) or gate mmap to the file being closed first.
- **Resident clean-page working set** during an allProxy search storm: bounded
  to the touched domain/IP sections; acceptable (clean, evictable), but worth
  observing with the `phys_footprint` instrumentation.

## Rollout / Sequencing

1. **k2-rules**: constitution doc + writer index sections + `Open`/`Close` mmap
   reader + invariant/parity tests. Tag a new version.
2. **k2-rules CI**: re-publish `all.krs.tar.gz` carrying the index sections;
   validate sort order at publish time.
3. **k2**: bump `k2-rules` dep; region-scoped `Open`; ①② boundary hoist;
   `e.tmp` discipline; engine `Close`; invariant integration test; CLAUDE.md
   reference.
4. **Foundation smoke** (separate, already-implemented instrumentation): rebuild
   NE, confirm `phys_footprint` baseline drops well under 50 MB and the connect
   loop is gone.

## Confidence

- Root cause: **10/10** (kernel `ActiveHard 50 MB (fatal)` + 19.2 MB measured).
- Target ~21 KB dirty heap, decoupled from rule count: **10/10** (byte-level
  accounting + additive-compatible format).
