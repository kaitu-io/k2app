# App Bypass Routes Unification — Master Spec

> **Status:** Approved (2026-05-27), revised after Plan A in-flight review (2026-05-27)
> **Supersedes:** Partial revision of `2026-05-25-app-bypass-engine-managed-design.md` (App Bypass v2 Phase 2). All Phase 2 logic now subsumed under the routes-unified model below.
> **Related plans:** `plans/2026-05-27-plan-a-krs-integration.md` · `plans/2026-05-27-plan-bc-outline.md`

## 1. Problem Statement

Three coupled defects in the current architecture:

### 1.1 Region drift
- Webapp `config.store.country` and daemon `appBypassOverrides.Region` are two stores with no convergence policy.
- Visible symptom: after `make dev-macos` fresh boot, UI shows "🇨🇳 中国" but daemon reports `region=""`. Smart-recognition silently disabled until user touches a setting.

### 1.2 Format split
- `cn-access.k2b` (binary, mmap, sorted IP ranges + domain suffixes) — owned by `k2/rule`.
- `app-bypass-cn.yaml` (plain text) — owned by `k2/appbypass`, separate loader, separate match codepath.
- Two distribution channels, two signing flows, two version cadences. PR review style differs (binary opaque vs YAML diffable).

### 1.3 API surface duplication
- Daemon's `provider.ListInstalled` (mac/Windows/Linux Go scanners) is invoked **only** by `app-bypass-preview`.
- Tauri shell's `list_running_apps` (NSWorkspace / sysinfo) is invoked by the AppBypass "Add more" picker.
- Two enumeration paths, two icon paths, two staleness models.
- Go-side enumeration is structurally wrong on Mac/Win: daemon runs as root with no GUI session, can't see app metadata cleanly.

## 2. Solution: Three-Layer Refactor

### Layer 1 — `.krs` unified bundle (Plan A: integrate `k2-rules/krs` library)

One `.krs` file per region (e.g. `cn.krs`, `ir.krs`), produced by the `k2-rules` repo's CI from per-region YAML sources. Contains all rule types in a single binary:

```
cn.krs
├── header        magic "K2RL" + version u16 + section count u16
├── section index count × 10 bytes (TypeID u16 + offset u32 + length u32)
├── SetTable                     0x0001  named routing sets ("geoip-cn", "domain-cn", ...)
├── IPv4RangesBySet              0x0010  per-set IPv4 ranges
├── IPv6RangesBySet              0x0011  per-set IPv6 ranges
├── DomainSuffixBySet            0x0012  per-set reversed-suffix list
├── DomainExcludeBySet           0x0013  per-set negative-suffix overrides
├── AndroidInstallers            0x0100  exact-match installer packages
├── AndroidApps                  0x0101  glob, case-sensitive
├── WindowsApps                  0x0200  glob, lowercased at compile
└── DarwinApps                   0x0300  glob, case-sensitive
```

**Format ownership lives in `k2-rules` repo** (`github.com/kaitu-io/k2-rules/krs`). k2 imports the library — no parallel format implementation:

```go
import "github.com/kaitu-io/k2-rules/krs"

bundles, _ := krs.Load(cacheDir)        // replaces rule.Load
idx        := krs.Index(bundles)         // replaces rule.Index
ok         := namedSet.MatchDomain(host) // replaces BundleSet.MatchDomain
ok         := namedSet.MatchIP(addr)     // replaces BundleSet.MatchIP
matched    := krs.MatchInstalled(bundle.Apps, apps, runtime.GOOS) // replaces appbypass.MatchInstalled
```

**API mapping (existing k2 surface → krs):**

| k2 (today) | krs (after Plan A) |
|---|---|
| `rule.Load(dir)`, `rule.ReadBundle(data)` | `krs.Load(dir)`, `krs.ReadBundle(data)` |
| `rule.Bundle`, `rule.BundleSet` | `krs.Bundle`, `krs.NamedSet` |
| `BundleSet.MatchDomain/MatchIP` | `NamedSet.MatchDomain/MatchIP` (same signatures) |
| `rule.Index(bundles)` | `krs.Index(bundles)` |
| `appbypass.Load`, `*Preset`, `MatchInstalled` | `bundle.Apps` (`*AppPatterns`), `krs.MatchInstalled` |
| `appbypass.AndroidPatterns.Package{Exact,Prefix}` | `AppPatterns.Android.Apps` (single-`*` glob) |
| `appbypass.DesktopPatterns.Process{Exact,Prefix}` | `AppPatterns.Windows.Apps` + `AppPatterns.Darwin.Apps` (platform-split, glob) |

Bundles are **unsigned** (CDN + manifest sha256 trust). Signing is a future cross-repo task.

**Migration policy (revised 2026-05-27):** k2 client cuts over to `.krs` **immediately** when Plan A merges. The legacy `.k2b` reader path inside k2 is deleted in Plan A Phase 3, not preserved for "6 months". This is safe because:

1. k2-rules CDN already ships both `.k2b` and `.krs` for every region (verified via `https://api.github.com/repos/kaitu-io/k2-rules/releases/latest` on 2026-05-27, 18 regions × 2 formats present).
2. k2-rules is a first-party repo with controlled release cadence, not a third-party dependency — there is no "old data we cannot upgrade" scenario.
3. Carrying parallel readers doubles surface area for bugs in transition without serving any production user.

Old `.k2b` files in client cache after upgrade are ignored (`krs.Load` filters by `.krs` suffix). They get garbage-collected on the next `EnsureBundles` swap (Plan A Phase 6 — `atomicSwap`'s old-file pruning loop will start treating `.k2b` as "not in new set, delete"). Brief degradation window (~10s, until first EnsureBundles tick) where smart-recognition falls back to global-mode behavior is acceptable.

### Layer 2 — Routes vocabulary unification (Plan B)

`ClientConfig.routes[]` becomes the only routing language. Wire schema:

```yaml
routes:
  # Tier 1: user-set per-app overrides (any region)
  - match: { apps: ['Firefox*'] },        via: 'k2v5://...'
  - match: { apps: ['Chrome.app'] },      via: direct

  # Tier 2: region (covers IP + domain + apps in one match)
  - match: { region: 'cn' },              via: direct

  # Tier 3: catch-all
  - match: { all: true },                 via: 'k2v5://...'
```

Match-field semantics:

| Field | Source | Match against |
|-------|--------|---------------|
| `apps: [glob, ...]` | webapp explicit | Process executable name or package name (platform-dependent, single field — different platforms don't coexist) |
| `region: 'cn'` | webapp explicit | Connection IP, connection domain, OR running process's app — any match in `cn.krs` triggers |
| `all: true` | catch-all | Always matches |

`ClientConfig.app_bypass` top-level field: **deleted**. All info flows through `routes[]`.

Old `match.preset` field: deprecated for region-named bundles (replaced by `match.region`). Kept temporarily for non-region presets if any emerge; YAGNI removal candidate.

### Layer 3 — UI + platform API redesign (Plan C)

AppBypass page (`/app-bypass`):
- Lists **all installed apps** from `_platform.list_installed_apps()`.
- Each row shows a "default direct" / "default proxy" badge — computed by webapp from current region selection + `cn.krs` patterns (via daemon `classify-apps` action for match consistency).
- User can force-override per app: **force proxy** or **force direct**.
- Override produces Tier-1 routes prepended on next `up`.

"More" section (collapsible): non-app running processes from `_platform.list_running_processes()`. User can manually add a running process to override list.

Platform API (replaces current `_platform.appList.listRunning`):
```ts
_platform.list_installed_apps(): Promise<InstalledApp[]>
_platform.list_running_processes(): Promise<RunningProcess[]>
```

Per-platform implementation:
| Platform | `list_installed_apps` | `list_running_processes` |
|----------|----------------------|--------------------------|
| macOS Tauri | `tauri-plugin-shell` + `system_profiler SPApplicationsDataType -json` | Existing NSWorkspace.runningApplications (renamed) |
| Windows Tauri | `tauri-plugin-shell` + `Get-ItemProperty HKLM:\...\Uninstall\* \| ConvertTo-Json` | Existing sysinfo (renamed) |
| Android Capacitor | K2Plugin `getInstalledApplications` (PackageManager) | K2Plugin `getRunningAppProcesses` |
| iOS | Returns `[]` (sandbox) | Returns `[]` |
| Linux daemon-served | Daemon `GET /api/installed-apps` (Linux-only HTTP) | Daemon `GET /api/running-processes` |

**Go-side `provider.ListInstalled` is deleted on mac/Win/Android.** Linux daemon-served keeps the Go scanner (`applist_linux.go`) because daemon **is** the platform shell on Linux (analogous to Tauri Rust on mac/Win). All other Go-side `applist_*.go` files are deleted in Plan C.

**InstalledApp shape (cross-platform contract):**

```ts
type InstalledApp = {
  id: string;                          // android packageName | desktop bundle id / exe path
  label: string;                       // human-readable
  icon_url?: string;                   // Tauri custom protocol url | data: url | undefined
  installer_package_name?: string;     // android only — needed by classify-apps
  process_names: string[];             // desktop: basenames; android: [id]
}
```

`installer_package_name` and `process_names` are mandatory inputs to `krs.MatchInstalled` (the engine-side classifier). Plan C platform implementations must populate them — they are not optional UI sugar.

## 3. Daemon API Changes

### Deleted actions
| Action | Replaced by |
|--------|------|
| `app-bypass-get` | (no replacement — state lives in webapp localStorage / ClientConfig) |
| `app-bypass-set-region` | (state derived from UI; flows through `up`'s ClientConfig) |
| `app-bypass-set-custom` | (state lives in webapp; flows through ClientConfig Tier 1) |
| `app-bypass-preview` | `classify-apps` (new, stateless) |

### New actions
- **`classify-apps`** — Stateless classifier. Takes `{region: string, installed: InstalledApp[]}`, returns `{classifications: [{id, default: 'direct'|'proxy', via_preset: string|null}]}`. Engine uses identical match codepath internally so UI and routing agree.
- **`get-presets`** — Discovery. Returns metadata of regions currently loaded (`{regions: [{id: 'cn', loaded: true, patterns: {...}}]}`) for UI selection menus.

## 4. Wire-Compat Plan

### ClientConfig
- `app_bypass` field: **deleted** (Plan B). One atomic ClientConfig wire bump.
- `routes[]`: extended with `match.region` and `match.apps` (Plan B). `match.preset` deprecated, retained.

### Bundle format
- `.krs`: read via imported `github.com/kaitu-io/k2-rules/krs` library — k2 has zero parallel reader/writer implementation. Plan A's deliverable is the import wiring + caller-site updates, not a new format.
- `.k2b`: legacy reader is **deleted in Plan A Phase 3** (no in-tree parallel reader, no transition coexistence). `k2-rules` CI keeps emitting `.k2b` alongside `.krs` for any third-party consumer of older k2 binaries (e.g., distro packages still pinned to v0.4.5), but k2's own client codepath stops reading them. Pre-existing `.k2b` files in client cache are ignored on load and pruned on next `atomicSwap` (Phase 6).

### State.json (daemon)
- `AppBypass.Region` field: marked `json:"-"` (Plan B), never persisted.
- `AppBypass.ProcessAdds` / `PackageAdds`: deleted from struct (Plan B), no longer used.

## 5. Plan Dependencies

```
Plan A (krs integration)
  └→ k2 imports github.com/kaitu-io/k2-rules/krs
     k2/rule/ Bundle/BundleSet/Load/Index code deleted (engine imports krs directly)
     k2/appbypass/ package deleted (callers use krs.MatchInstalled)
     EnsureBundles downloads .krs alongside .k2b during transition
       │
       └→ Plan B (routes vocab)
             ├→ ClientConfig.app_bypass removed
             ├→ buildConnectConfig emits match.region/match.apps
             ├→ daemon classify-apps action
             └→ daemon delete app-bypass-* actions
                   │
                   └→ Plan C (page + platform)
                         ├→ _platform.list_installed_apps + list_running_processes
                         ├→ delete provider.ListInstalled (mac/win/android Go)
                         ├→ AppBypass page redesign
                         └→ Tier-1 force overrides in routes
```

Plans must merge in order. Each plan ships forward-compatible (newer daemon reads older client; we never need backward-read).

## 6. Acceptance Criteria

Per Plan, see individual plan docs. Master-level "done" gate:

1. ✅ Region drift bug ([§1.1](#11-region-drift)) cannot recur (single source of truth in webapp localStorage + Tier 2 region route).
2. ✅ `k2/rule/` no longer contains Bundle/BundleSet/Load/Index — engine imports `krs` directly. `k2/appbypass/` package deleted. Client disk holds `.krs` files served by k2-rules CDN.
3. ✅ Daemon has zero `app-bypass-*` actions; zero `provider.ListInstalled` calls on mac/win/android.
4. ✅ AppBypass page can express force-proxy override (regression-test against previous "direct only" model).
5. ✅ Manual smoke (3 plans × their own smoke each).
6. ✅ **CDN gate**: `https://github.com/kaitu-io/k2-rules/releases/latest/download/k2-rules.tar.gz` contains `.krs` files for every region listed in `k2/rule/target.go presets` map. Verified at Plan A Phase 0 and re-verified before final Plan A merge.
7. ✅ **Upgrade-path smoke**: existing v0.4.5 install (with `.k2b` cache) → new binary → connect → confirm `EnsureBundles` prunes stale `.k2b`, loads `.krs`, smart recognition active within 10s.

## 7. Out of Scope

- iOS App Bypass — sandbox doesn't allow per-app attribution at the network layer. Spec rev 2 already excludes iOS; remains excluded.
- Cross-region bundle composition (e.g., "match CN apps but use TH IP rules") — explicit one-region-per-route model.
- Per-route encryption / fingerprinting variation — orthogonal to App Bypass.

## 8. Risks

| Risk | Likelihood | Mitigation |
|------|-----------:|------------|
| krs library v0.1.0 API shifts before stabilization | Medium (explicitly pre-1.0 per k2-rules tag annotation) | Pin to exact version (`require ... v0.1.0`) in k2 go.mod; coordinate any API changes via PR to k2-rules first. |
| Plan B ClientConfig schema break leaves mobile users with `app_bypass.region` in stored config | Low (mobile localStorage is webapp-controlled, will be cleared on first new-build start) | One-shot localStorage migration in webapp boot: read old `k2.advanced.app_bypass`, push to new `k2.routes.overrides`, then delete. |
| Webapp's `classify-apps` IPC adds latency to AppBypass page render | Low | Single batch call per region selection; expected <50ms for typical 50-200 installed-app list. Cached in webapp store. |
| Post-upgrade cache rollover: client has `.k2b` files in cache from pre-Plan-A binary, new binary's `krs.Load` returns empty until `EnsureBundles` refreshes | Low | First `EnsureBundles` tick on connect (or on daemon start when cache age exceeds threshold) re-downloads tarball, extracts `.krs`, prunes old `.k2b`. Window: <10s after first connect. Smart recognition degrades to global-mode behavior in this window (still functional, just not optimized). |
| k2-rules CDN regresses and drops `.krs` from a future release | Low | Plan A Phase 0 verifies CDN; release-process owner adds `.krs` files to k2-rules CI smoke. If detected post-merge, client cache retains last good `.krs` (atomicSwap is all-or-nothing) — degrades only after manifest sha256 forces re-download of a broken set. |

## 9. Vocabulary

- **Region**: ISO 3166-1 alpha-2 country code OR a future logical grouping (currently always a country). `cn`, `th`, `us`. Wire format: lowercase 2-char.
- **Region bundle**: `.krs` file containing all rule types for one region.
- **Named set**: One entry inside a `.krs` bundle's SetTable (e.g. `geoip-cn`, `domain-cn`). Routes reference these by name.
- **Override**: User-set Tier-1 force-proxy or force-direct rule for a specific app.
- **Classification**: Engine-derived answer for "given current region + this app, where does it go?". Returned by `classify-apps`.
- **Preset**: Legacy term, used in pre-Plan-B routes for `cn-access`. Phased out for region-based matches; reserved for non-region named bundles if any emerge.
- **App**: A user-installed application (bundle on macOS, exe on Windows, package on Android). Distinct from **process** (a running OS process; may not correspond to an installed app).
