# Rule Module Phase 2: Bundle Downloader, Hot Reload, Android PackageResolver

## Goal

Complete the rule/ module with three capabilities: (1) automatic k2b bundle downloading from multiple CDN sources with Happy Eyeballs racing, (2) runtime hot-swap of rule engine without restart, (3) Android package name resolution via Go→Java callback for per-route app matching.

## Architecture

Three independent modules, composed at engine startup:

```
engine.Start()
  ├─ rule.Download(cacheDir, sources)     // [1] blocking if empty, background if cached
  │    └─ Happy Eyeballs: race N sources, first 200 wins
  ├─ rule.Load(cacheDir)                  // existing: load k2b from disk
  ├─ rule.NewEngine(routes, bundles...)   // existing: build engine
  └─ rule.StartUpdater(engine, cacheDir)  // [2] background 24h check → download → engine.Reload()

provider/process_linux.go
  └─ FindProcess(network, srcIP, srcPort)
       └─ UID ≥ 10000 → PackageResolver.PackageForUID(uid)  // [3] Go→Java JNI
```

## Feature 1: k2b Bundle Auto-Downloader

### File: `rule/downloader.go`

### Download Sources

Hardcoded defaults (config `rule_sources` overrides):

```go
var defaultSources = []string{
    "https://github.com/kaitu-io/k2-rules/releases/latest/download/",
    "https://cdn-mirror-1.example.com/k2-rules/latest/",
    "https://cdn-mirror-2.example.com/k2-rules/latest/",
}
```

Bundles to download: `overseas.k2b`, `cn-direct.k2b` (derived from route config — only download bundles referenced by `match.preset` or `match.names`).

### Happy Eyeballs Racing

For each bundle file:
1. Spawn one goroutine per source URL, all start simultaneously.
2. Each sends HTTP GET with `If-Modified-Since: <local file mtime>`.
3. First to return 200 → save body, cancel all others via shared context.
4. 304 from any source → file is current, cancel all others.
5. All fail → retry with exponential backoff (1s, 2s, 4s, 8s… cap 60s).

### First Boot vs Subsequent

- **cache_dir empty** (no k2b files for referenced bundles): `Download()` blocks, retries indefinitely until success. Engine startup waits.
- **cache_dir has files**: Engine starts immediately with cached files. Background goroutine checks for updates.

### File Write Safety

Write to `{name}.k2b.tmp`, then `os.Rename()` for atomic replacement. Prevents readers from seeing partial files.

### Config Integration

```yaml
# config.yaml (optional — most users never set this)
rule_sources:
  - "https://custom-mirror.example.com/k2-rules/"
```

`config.ClientConfig` gains:

```go
RuleSources []string `yaml:"rule_sources,omitempty" json:"rule_sources,omitempty"`
```

### API

```go
// Download ensures all referenced k2b bundles exist in cacheDir.
// Blocks until all bundles are present. Uses Happy Eyeballs across sources.
// Returns nil when all bundles are cached (existing or freshly downloaded).
func Download(ctx context.Context, cacheDir string, bundles []string, sources []string) error

// bundlesForConfig returns the list of k2b bundle names referenced by routes.
func bundlesForConfig(routes []config.RouteConfig) []string
```

## Feature 2: Hot Reload (rule.Engine Internal Swap)

### File: `rule/engine.go` (extend existing)

### Trigger

Background updater (`rule/updater.go`) runs every 24h:
1. Call `Download()` with `If-Modified-Since` — returns quickly if 304.
2. If any file was updated → call `engine.Reload(cacheDir, routes)`.
3. If download fails → log warn, keep running with current rules.

### Reload Method

```go
// Reload replaces the engine's bundles and routes atomically.
// Caller provides the cacheDir and current route config.
// Thread-safe: uses WLock. Match/MatchConn callers block briefly during swap.
func (e *Engine) Reload(cacheDir string, routes []config.RouteConfig) error
```

Implementation:
1. Load new bundles from cacheDir (outside lock — I/O is lock-free).
2. Build new RouteEntry list from routes + new bundle index.
3. `e.mu.Lock()` — swap `e.routes`, `e.bundles`, clear `e.tmp` map, `e.mu.Unlock()`.
4. Close old bundles outside lock.

### TmpRule Clearing

On swap, `e.tmp` is reset to empty map. Rationale: TmpRules are DNS-learned IP→target mappings based on old rule sets. New rules may route the same domains differently. DNS queries will re-populate tmp naturally.

### Updater Goroutine

```go
// StartUpdater launches a background goroutine that checks for bundle updates
// every 24h. Stops when ctx is cancelled. Calls engine.Reload() on success.
func StartUpdater(ctx context.Context, engine *Engine, cacheDir string,
    routes []config.RouteConfig, sources []string)
```

Lifecycle: started by `engine.Engine.Start()`, cancelled by `engine.Engine.Stop()` via context.

### Error Handling

- Download failure → log warn, no swap, old engine continues.
- Load/build failure → log warn, no swap, old engine continues.
- Never crashes, never leaves engine in broken state.

## Feature 3: Android PackageResolver

### Interface: `appext/appext.go`

```go
// PackageResolver resolves Android UID to package name.
// Implemented by native Android layer (Java/Kotlin).
// gomobile exports this as a callback interface.
type PackageResolver interface {
    PackageForUID(uid int32) string
}
```

gomobile will export this as a Java interface. The Android app implements it:

```java
// Java side (in VpnService)
public class K2PackageResolver implements Appext.PackageResolver {
    private final Context context;
    
    public String packageForUID(int uid) {
        String[] packages = context.getPackageManager().getPackagesForUid(uid);
        if (packages != null && packages.length > 0) {
            return packages[0];
        }
        return "";
    }
}
```

### Wiring

```
EngineConfig.PackageResolver (appext)
  → engine.Config.PackageResolver (engine)
    → ProcessSearcher constructor (provider)
      → LinuxProcessSearcher.packageResolver field
        → FindProcess(): UID ≥ 10000 → packageResolver.PackageForUID(uid)
```

### Type Threading

| Layer | Field | Type |
|-------|-------|------|
| `appext.EngineConfig` | `PackageResolver` | `PackageResolver` (gomobile interface) |
| `engine.Config` | `PackageResolver` | `provider.PackageResolver` (Go interface) |
| `provider` package | `PackageResolver` interface | `PackageForUID(uid int32) string` |
| `LinuxProcessSearcher` | `packageResolver` field | `provider.PackageResolver` |

The `provider.PackageResolver` interface is defined in `provider/process.go` (same package as ProcessSearcher). `appext.PackageResolver` wraps it for gomobile export.

### Platform Behavior

| Platform | PackageResolver | Behavior |
|----------|----------------|----------|
| Android | Provided by Java layer | UID → package name via Android API |
| Linux desktop | nil | packageName always "" |
| macOS/iOS | N/A (process_darwin.go) | Uses lsof, no UID concept |
| Windows | N/A (stub) | No process search |

### Caching

`process_linux.go` already caches FindProcess results for 2 seconds keyed by `(network, srcIP, srcPort)`. Same connection reuses cached result — no repeated JNI calls.

### Graceful Degradation

- PackageResolver nil → skip package lookup, packageName = ""
- PackageForUID returns "" → same as above
- Route with `package_name` criteria → won't match, falls through to next route
- No crash, no error — just reduced routing expressiveness on unsupported platforms

## Dependencies Between Features

```
[1] Downloader ──→ [2] Hot Reload (updater calls downloader, then reload)
                        ↑ independent
[3] PackageResolver ────┘ (no dependency on 1 or 2)
```

Features 1+2 are tightly coupled (updater = downloader + reload). Feature 3 is fully independent.

## Files to Create/Modify

### New Files
- `rule/downloader.go` — Download(), Happy Eyeballs racing, HTTP caching
- `rule/downloader_test.go` — httptest server, race conditions, retry logic
- `rule/updater.go` — StartUpdater(), 24h background loop
- `rule/updater_test.go` — timer-based tests with fake clock

### Modified Files
- `rule/engine.go` — add Reload() method
- `rule/engine_test.go` — Reload tests (swap bundles, verify Match results change)
- `config/config.go` — add RuleSources field to ClientConfig
- `engine/engine.go` — wire downloader in Start(), start updater goroutine
- `engine/config.go` — add PackageResolver field
- `provider/process.go` — add PackageResolver interface
- `provider/process_linux.go` — inject PackageResolver, call on UID ≥ 10000
- `provider/process_test.go` — test with mock PackageResolver
- `appext/appext.go` — add PackageResolver to EngineConfig, wire through

## Testing Strategy

| Component | Test approach |
|-----------|--------------|
| Downloader | httptest server returning k2b bytes; test 304, racing, retry, atomic write |
| Happy Eyeballs | httptest with delayed servers; verify fastest wins, others cancelled |
| Reload | Build engine with bundle A, Reload with bundle B, verify Match() returns new results |
| TmpRule clearing | SetTmpRule before Reload, verify tmp is empty after |
| Updater | Fake timer, verify download+reload called on tick |
| PackageResolver | Mock interface returning known package names, verify FindProcess output |
| Integration | Download real k2b from GitHub (build tag `integration`), load, match known domains |
