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
depends on `k2-rules v0.1.1`) plus `krs.tar.gz` (every region in one archive)
made `engine.buildRuleEngine` call `rule.LoadNamed` → `os.ReadFile` + full
in-heap expand of **all 19 region bundles**, retained resident for the tunnel
lifetime via `Engine.bundles`, even under `allProxy` where rules are never
consulted. Commit `63e25b1` had deleted the per-region selectivity
(`BundleByRegion`).

### Measured data (the proof)

`krs.tar.gz` = 19 regions, 6.0 MB decompressed. Section-index parse:

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

Binding and CI-enforced. To be written verbatim to
`k2-rules/krs/CONSTITUTION.md` and referenced from `k2/rule/CLAUDE.md`. The
numeric budgets below are normative: a change that exceeds them fails CI and
must not merge.

> **krs Memory Constitution** (binding)
>
> 1. **On disk only.** The rule corpus **and all indexes** live on disk.
>    Runtime access is exclusively via read-only `mmap` (clean / file-backed
>    pages — excluded from, or trivially reclaimable under, iOS jetsam
>    `phys_footprint`).
> 2. **Heap invariant (testable, CI-gated).** A loaded bundle's resident dirty
>    heap MUST be `O(set count)`, never `O(rule count)`. **Budgets:** marginal
>    dirty heap per loaded region `< 8 KB`; total rule-attributable dirty heap
>    for *any* config `< 64 KB`, independent of corpus size or region count.
> 3. **Load only what is referenced.** Open exactly the regions referenced by
>    the active config (`match.region` ∪ `overseas`). Never the whole corpus.
> 4. **Never touch the whole mapping** on the constrained path: no full scan,
>    `canonicalize`, re-sort, or checksum of a mapped bundle (each would fault
>    and dirty the entire file, defeating mmap). Trust the producer; validate
>    structure at publish time in k2-rules CI.
> 5. **Hot path O(1).** Normalize/reverse the query **once** at the engine
>    boundary; matching allocates a small constant per lookup, never per-set,
>    never per-rule.
>
> **Prohibited on the client runtime path (NE / Service / daemon):**
> - Calling `krs.Load`, `krs.LoadNamed`, or `krs.ReadBundle` (all full-expand).
> - Any `[]string` domain table or `[][]byte` IP table held resident.
> - Loading a region not referenced by the active config.
> - Silently falling back to full-expand when an index is missing/corrupt —
>   the bundle MUST error instead (a silent fallback reintroduces the OOM).
> - Pre-faulting / `madvise(WILLNEED)` the whole mapping.
>
> Rationale: the iOS NE has a hard 50 MB `ActiveHard` jetsam ceiling. Read-only
> mmap converts rule data from counted dirty heap into reclaimable clean
> file-backed pages — the only way to fit a growing corpus in a fixed budget.

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

### Wire Format Extension (normative)

The format already silently skips unknown section TypeIDs, so the extension is
**additive and backward-compatible**: old `ReadBundle` ignores the new sections;
the new `Open` requires them. `krs.tar.gz` grows ~4 bytes/domain (~1 MB raw,
far less after gzip). No released client ships the v0.4.5 embed build yet, so
there is no in-field consumer to break.

**New section TypeIDs (append-only; values fixed here):**

```
typeDomainSuffixIndex  uint16 = 0x0014   // index for 0x0012 DomainSuffixBySet
typeDomainExcludeIndex uint16 = 0x0015   // index for 0x0013 DomainExcludeBySet
```

**Index section payload layout** (little-endian):

```
u16            setCount
setCount × {                          // per-set directory (mmap-read, O(sets))
  u32  entryStart   // index of this set's first entry in the offset table
  u32  entryCount   // number of domain entries for this set
}
N × u32        offset                  // N = total domain entries, in the SAME
                                       // (set_idx ASC, value ASC) order as the
                                       // companion domain payload. Each offset
                                       // is RELATIVE to the domain section
                                       // payload start, pointing at that
                                       // entry's [u16 set_idx][uvarint len][bytes].
```

A bundle that emits a `DomainSuffixBySet` (0x0012) section **MUST** also emit its
`DomainSuffixIndex` (0x0014); likewise 0x0013 ⇒ 0x0015. `Open` errors if a
domain payload is present without its index (constitution rule: no silent
full-expand fallback). The producer (`WriteBundle`) emits both unconditionally.

**IP sections need no extension.** `typeIPv4RangesBySet` / `typeIPv6RangesBySet`
are already fixed-width (`2 + 2·addrLen` bytes/entry) and sorted by
`(set_idx, start)`, so per-set blocks and the in-block ranges are both
**binary-searchable in place on mmap** by index arithmetic — zero index, zero
heap.

### mmap Match Algorithm (normative)

All reads bounds-checked against the mapped length; an out-of-bounds offset is a
load-time error (see Error Handling), never a runtime panic.

- **Domain (set S):** read S's `{entryStart, entryCount}` from the index
  directory; binary-search that offset slice (`entryCount` `u32`s) by comparing
  the query's reversed parent suffix against the entry bytes at
  `domainPayload[offset:]` (skip `u16 set_idx` + `uvarint len`, read `len`
  bytes). Walk the L parent suffixes exactly as today's `domainSection.Match`.
  Heap per lookup: O(1) (the normalized/reversed query, built once at the engine
  boundary). Excludes checked first via the 0x0015 index.
- **IP (set S):** binary-search the fixed-width IP section for S's
  `[start,end)` block by reading `u16 set_idx` at `payload[i·entrySize:]`, then
  binary-search ranges within the block (`bytes.Compare` against mmap'd
  `start`/`end`). Identical semantics to today's `ipRangeSection.Contains`,
  operands read from mmap.

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
  consulted). Otherwise bound the map to a hard cap of **4096** entries with
  drop-newest-on-full (or per-session reset on `Stop`); it must never grow
  unbounded over a long session.
- **Universality**: the mmap path is the single path on **all** platforms
  (desktop daemon included) — no `ReadBundle` runtime fork. Desktop has no 50 MB
  limit, but one code path is simpler and the constitution holds everywhere.
- **Import boundary**: `appext` and `engine` runtime packages MUST NOT reference
  `krs.Load`/`LoadNamed`/`ReadBundle`. Enforced by a test that greps the build
  import graph (see Enforcement).

### Embed & updater must carry the index

`make fetch-rules-embed` and the CDN `krs.tar.gz` must be regenerated by the
new producer so every shipped `.krs` carries its index sections. `SeedFromEmbed`
and the background updater extract bytes verbatim (no re-encode), so they
propagate the index automatically — but the **embedded blob in the binary must
be re-fetched** as part of this change, or `Open` errors on a cold-start seed.

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
  force `runtime.GC()`, assert `HeapInuse` delta is within the normative budget
  (`< 64 KB` total; `< 8 KB` marginal per added region) vs `Open` of an empty
  dir. The executable form of invariant #2; lives in **both** k2-rules (`Open`
  unit) and k2 (engine integration). HeapInuse is span-coarse, so the test
  loads several regions and asserts the *slope* stays flat (O(sets)), not just
  the absolute delta.
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

## Enforcement & CI Gates

The constitution binds only if violations fail CI. Required gates:

- **Heap-budget gate** (both repos): the invariant test asserts the normative
  budgets — marginal dirty heap per loaded region `< 8 KB`, total
  rule-attributable dirty heap `< 64 KB` for any config — measured via
  `HeapInuse` deltas after `runtime.GC()`. Merge-blocking.
- **Allocation gate**: `testing.AllocsPerRun(Engine.Match)` ≤ a fixed small
  constant (e.g. ≤4), asserted in a `-benchmem` test. Catches reintroduced
  per-set/per-rule allocation.
- **Import-boundary gate**: a test that fails if the `engine`/`appext` runtime
  build graph imports `krs.Load`/`LoadNamed`/`ReadBundle` (the full-expand
  entry points). This is the mechanical form of the "Prohibited" list.
- **Format-validity gate** (k2-rules CI): every published `.krs` is parsed and
  its index sections validated — present-when-domain-present, sorted, offsets
  in-bounds — so the runtime can trust without scanning.
- **Producer parity gate**: `WriteBundle → Open` and `WriteBundle → ReadBundle`
  produce identical match results on a fixed corpus.

A future change that needs to exceed a budget must amend this spec + the
`CONSTITUTION.md` in the same PR, with justification — the numbers are not
silently adjustable.

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
2. **k2-rules CI**: re-publish `krs.tar.gz` carrying the index sections;
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
