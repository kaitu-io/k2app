# App Bypass Routes Unification — Master Spec

> **Status:** Approved (2026-05-27)
> **Supersedes:** Partial revision of `2026-05-25-app-bypass-engine-managed-design.md` (App Bypass v2 Phase 2). All Phase 2 logic now subsumed under the routes-unified model below.
> **Related plans:** `plans/2026-05-27-plan-a-k2b2-format.md` · `plans/2026-05-27-plan-b-routes-unification.md` · `plans/2026-05-27-plan-c-appbypass-page.md`

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

### Layer 1 — `.k2b2` unified binary bundle (Plan A)

One file per region containing ALL rule types:

```
cn.k2b2
├── header (K2B2 magic + version + region + section index)
├── ip_v4_ranges        ← sorted CIDR (binary search)
├── ip_v6_ranges        ← sorted CIDR
├── domain_suffixes     ← sorted reverse-string suffixes
├── android_installers  ← exact match list
├── android_apps        ← glob patterns
├── windows_apps        ← glob patterns (lowercased)
└── darwin_apps         ← glob patterns
```

Loaded once via mmap. Sections accessed independently; small per-section overhead. Signed via existing `.sig` detached-signature flow.

YAML source lives in k2-rules repo; CI compiles to `.k2b2`. Client only reads.

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
| `region: 'cn'` | webapp explicit | Connection IP, connection domain, OR running process's app — any match in `cn.k2b2` triggers |
| `all: true` | catch-all | Always matches |

`ClientConfig.app_bypass` top-level field: **deleted**. All info flows through `routes[]`.

Old `match.preset` field: deprecated for region-named bundles (replaced by `match.region`). Kept temporarily for non-region presets if any emerge; YAGNI removal candidate.

### Layer 3 — UI + platform API redesign (Plan C)

AppBypass page (`/app-bypass`):
- Lists **all installed apps** from `_platform.list_installed_apps()`.
- Each row shows a "default direct" / "default proxy" badge — computed by webapp from current region selection + `cn.k2b2` patterns (via daemon `classify-apps` action for match consistency).
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

**Go-side `provider.ListInstalled` is deleted on mac/Win/Android.** Linux daemon-served stays because daemon **is** the platform shell on Linux (analogous to Tauri Rust on mac/Win).

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
- `.k2b`: read-only fallback for one release after `.k2b2` ships (Plan A). Once `.k2b2` is universal, `.k2b` reader deleted.
- `.k2b2`: produced by k2-rules CI. Compiles same YAML source as today but per-region (not per-rule-type).

### State.json (daemon)
- `AppBypass.Region` field: marked `json:"-"` (Plan B), never persisted.
- `AppBypass.ProcessAdds` / `PackageAdds`: deleted from struct (Plan B), no longer used.

## 5. Plan Dependencies

```
Plan A (.k2b2)
  └→ rule engine has unified bundle reader; appbypass package merged in
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
2. ✅ `cn-access.k2b` + `app-bypass-cn.yaml` no longer exist on client disk; only `cn.k2b2` + `.sig`.
3. ✅ Daemon has zero `app-bypass-*` actions; zero `provider.ListInstalled` calls on mac/win/android.
4. ✅ AppBypass page can express force-proxy override (regression-test against previous "direct only" model).
5. ✅ Manual smoke (3 plans × their own smoke each).

## 7. Out of Scope

- iOS App Bypass — sandbox doesn't allow per-app attribution at the network layer. Spec rev 2 already excludes iOS; remains excluded.
- Cross-region bundle composition (e.g., "match CN apps but use TH IP rules") — explicit one-region-per-route model.
- Per-route encryption / fingerprinting variation — orthogonal to App Bypass.

## 8. Risks

| Risk | Likelihood | Mitigation |
|------|-----------:|------------|
| `.k2b2` mmap implementation has portability issues (Windows file-mapping vs Unix mmap) | Medium | Use `mmap-go` or existing k2 `rule/domain.go` pattern (already does mmap). Test fixtures on all 3 desktop OSes in CI. |
| Plan B ClientConfig schema break leaves mobile users with `app_bypass.region` in stored config | Low (mobile localStorage is webapp-controlled, will be cleared on first new-build start) | One-shot localStorage migration in webapp boot: read old `k2.advanced.app_bypass`, push to new `k2.routes.overrides`, then delete. |
| Webapp's `classify-apps` IPC adds latency to AppBypass page render | Low | Single batch call per region selection; expected <50ms for typical 50-200 installed-app list. Cached in webapp store. |
| k2-rules CI doesn't exist or doesn't have `.k2b2` compiler | Medium | Plan A delivers Go-side compiler in `cmd/k2b2compile` usable by k2-rules CI. Stop-gap: keep emitting both `.k2b` and `.k2b2` until production CI is wired. |

## 9. Vocabulary

- **Region**: ISO 3166-1 alpha-2 country code OR a future logical grouping (currently always a country). `cn`, `th`, `us`. Wire format: lowercase 2-char.
- **Region bundle**: `.k2b2` file containing all rule types for one region.
- **Override**: User-set Tier-1 force-proxy or force-direct rule for a specific app.
- **Classification**: Engine-derived answer for "given current region + this app, where does it go?". Returned by `classify-apps`.
- **Preset**: Legacy term, used in pre-Plan-B routes for `cn-access`. Phased out for region-based matches; reserved for non-region named bundles if any emerge.
- **App**: A user-installed application (bundle on macOS, exe on Windows, package on Android). Distinct from **process** (a running OS process; may not correspond to an installed app).
