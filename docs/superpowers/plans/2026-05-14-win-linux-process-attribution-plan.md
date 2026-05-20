# Win/Linux Desktop ProcessSearcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `provider.ProcessSearcher` for Windows + Linux desktop daemon so App Bypass `process_name` rules match on those platforms; refactor Darwin/Linux/Windows around a shared cache helper with TTL + soft cap.

**Architecture:** Build-tag-free `processCache` helper (TTL + soft-cap lazy eviction) shared by all three platforms. Linux: stale-while-revalidate inode→PID reverse index (5s TTL) + singleflight to amortize `/proc/*/fd` walks. Windows: pure-Go syscall to `GetExtendedTcpTable`/`GetExtendedUdpTable` + `OpenProcess`/`QueryFullProcessImageName` behind an interface seam for unit testing.

**Tech Stack:** Go 1.x stdlib, `golang.org/x/sys/windows`, `golang.org/x/sync/singleflight`, `github.com/sasha-s/go-deadlock` (k2 mutex convention), `github.com/kaitu-io/k2/safego` (panic-safe goroutines).

**Spec:** [`docs/superpowers/specs/2026-05-14-win-linux-process-attribution-design.md`](../specs/2026-05-14-win-linux-process-attribution-design.md)

---

## Working Directory

All work happens in the k2 submodule under the worktree root:

```
/Users/david/projects/kaitu-io/k2app/.claude/worktrees/v0.4.5+app-bypass/k2/
```

Run all `go` commands and git operations from inside `k2/` (the submodule has its own git history). Final task bumps the submodule pointer in the parent worktree.

## Cross-Platform Test Execution

The plan creates code on three platforms but the dev machine is typically macOS. Build verification works cross-platform, but **runtime execution of platform-tagged tests requires that platform**:

| Test File | Build Tag | Where it Runs |
|-----------|-----------|---------------|
| `process_cache_test.go` | none | Any platform (incl. macOS dev) |
| `process_test.go` | `darwin && !ios` | macOS only |
| `process_linux_test.go` | `linux` | Linux only |
| `process_windows_test.go` | `windows` | Windows only |

**For each platform without local hardware:**

- **Linux from macOS dev:** Run tests inside Docker:
  ```bash
  docker run --rm -v "$(pwd):/work" -w /work/k2 --platform linux/amd64 \
    golang:1.22 go test -race ./provider/...
  ```
- **Windows from macOS dev:** Cross-compile only — `GOOS=windows go vet ./...` + `GOOS=windows go build ./...`. Runtime test deferred to Windows dev box or future CI runner.

The plan's test steps assume host can run the test. When the host is macOS:
- macOS tests (`process_test.go`, `process_cache_test.go`) — run directly
- Linux tests — run via Docker (per command above)
- Windows tests — vet/build only; runtime test marked as manual in §12.2 of spec

---

## File Structure

```
k2/provider/
  process.go             [unchanged] interface + NopProcessSearcher
  process_cache.go       [NEW] shared cache helper (build-tag free)
  process_cache_test.go  [NEW] cache tests
  process_darwin.go      [MOD] use shared cache
  process_linux.go       [REWRITE] cache + inode index + procRoot
  process_linux_test.go  [REWRITE] fake procfs + concurrent tests
  process_windows.go     [NEW] WindowsProcessSearcher + helpers + seams
  process_windows_test.go[NEW] mock fetcher + byte-order unit tests

k2/daemon/
  process_darwin.go      [unchanged]
  process_linux.go       [NEW] factory returns NewLinuxProcessSearcher(nil)
  process_windows.go     [NEW] factory returns NewWindowsProcessSearcher()
  process_other.go       [MOD] build tag !darwin && !linux && !windows

k2/CLAUDE.md             [MOD] DIAG event + Lock Ordering Graph entries
```

---

## Phase 1 — Shared Foundation

### Task 1: Shared `processCache` helper

**Files:**
- Create: `k2/provider/process_cache.go`
- Create: `k2/provider/process_cache_test.go`

- [ ] **Step 1.1: Write the failing test file**

Create `k2/provider/process_cache_test.go`:

```go
package provider

import (
    "net/netip"
    "testing"
    "time"
)

func TestCacheKey_Format(t *testing.T) {
    got := cacheKey("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    want := "tcp:192.168.1.5:54321"
    if got != want {
        t.Errorf("cacheKey = %q, want %q", got, want)
    }
}

func TestProcessCache_HitAfterSet(t *testing.T) {
    c := newProcessCache(16, time.Second)
    c.set("k", "chrome", "")
    name, pkg, hit := c.get("k")
    if !hit || name != "chrome" || pkg != "" {
        t.Errorf("get = (%q,%q,%v), want (chrome,,true)", name, pkg, hit)
    }
}

func TestProcessCache_MissOnEmpty(t *testing.T) {
    c := newProcessCache(16, time.Second)
    _, _, hit := c.get("nope")
    if hit {
        t.Errorf("expected miss on empty cache")
    }
}

func TestProcessCache_NegativeEntryStored(t *testing.T) {
    c := newProcessCache(16, time.Second)
    c.set("k", "", "")  // negative cache
    name, pkg, hit := c.get("k")
    if !hit || name != "" || pkg != "" {
        t.Errorf("get = (%q,%q,%v), want (,,true)", name, pkg, hit)
    }
}

func TestProcessCache_TTLExpiry(t *testing.T) {
    c := newProcessCache(16, 10*time.Millisecond)
    c.set("k", "chrome", "")
    time.Sleep(20 * time.Millisecond)
    _, _, hit := c.get("k")
    if hit {
        t.Errorf("expected expiry after TTL")
    }
}

func TestProcessCache_SoftCapEvictsExpired(t *testing.T) {
    c := newProcessCache(2, 10*time.Millisecond)
    c.set("k1", "a", "")
    c.set("k2", "b", "")
    time.Sleep(20 * time.Millisecond)  // both expire
    c.set("k3", "c", "")  // len >= cap triggers sweep
    // After sweep, k1/k2 (expired) removed; k3 added.
    if _, _, hit := c.get("k1"); hit {
        t.Errorf("k1 should have been evicted")
    }
    if _, _, hit := c.get("k3"); !hit {
        t.Errorf("k3 should be present")
    }
}
```

- [ ] **Step 1.2: Run tests, verify they fail with build error**

Run from `k2/`:

```bash
go test ./provider/ -run TestProcessCache -run TestCacheKey
```

Expected: FAIL with "undefined: newProcessCache", "undefined: cacheKey", "undefined: processCache".

- [ ] **Step 1.3: Create `process_cache.go`**

Create `k2/provider/process_cache.go`:

```go
package provider

import (
    "net/netip"
    "strconv"
    "strings"
    "time"

    "github.com/sasha-s/go-deadlock"
)

const (
    processCacheCap = 4096
    processCacheTTL = 2 * time.Second
)

// cacheKey builds a stable string key for (network, srcIP, srcPort).
// Pre-allocated builder avoids fmt.Sprintf allocations on hot path.
func cacheKey(network string, srcIP netip.Addr, srcPort uint16) string {
    var b strings.Builder
    b.Grow(len(network) + 1 + 15 + 1 + 5)
    b.WriteString(network)
    b.WriteByte(':')
    b.WriteString(srcIP.String())
    b.WriteByte(':')
    b.WriteString(strconv.FormatUint(uint64(srcPort), 10))
    return b.String()
}

type processCache struct {
    mu      deadlock.Mutex
    entries map[string]processCacheEntry
    cap     int
    ttl     time.Duration
}

type processCacheEntry struct {
    name    string
    pkg     string
    expires time.Time
}

func newProcessCache(cap int, ttl time.Duration) *processCache {
    return &processCache{
        entries: make(map[string]processCacheEntry),
        cap:     cap,
        ttl:     ttl,
    }
}

func (c *processCache) get(key string) (name, pkg string, hit bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    e, ok := c.entries[key]
    if !ok || time.Now().After(e.expires) {
        return "", "", false
    }
    return e.name, e.pkg, true
}

func (c *processCache) set(key, name, pkg string) {
    now := time.Now()
    c.mu.Lock()
    defer c.mu.Unlock()
    if len(c.entries) >= c.cap {
        // Lazy sweep: amortized O(1), worst-case O(cap) every cap inserts.
        for k, e := range c.entries {
            if now.After(e.expires) {
                delete(c.entries, k)
            }
        }
    }
    c.entries[key] = processCacheEntry{
        name:    name,
        pkg:     pkg,
        expires: now.Add(c.ttl),
    }
}
```

- [ ] **Step 1.4: Run tests, verify they pass**

```bash
go test ./provider/ -run TestProcessCache -run TestCacheKey -v
```

Expected: PASS for all 6 tests.

- [ ] **Step 1.5: Commit**

```bash
git -C k2 add provider/process_cache.go provider/process_cache_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "feat(provider): shared processCache helper with TTL + soft-cap eviction" \
  -m "Replaces ad-hoc Darwin cache with build-tag-free helper. Linux/Windows
ProcessSearchers will share this in subsequent commits. Soft cap 4096 +
2s TTL bounds memory at ~200KB worst case."
```

---

### Task 2: Refactor Darwin to use shared cache

**Files:**
- Modify: `k2/provider/process_darwin.go`

- [ ] **Step 2.1: Read existing Darwin tests to know what must keep passing**

```bash
git -C k2 grep -l "TestDarwinProcessSearcher\|DarwinProcessSearcher\|TestProcessSearcher_SkipsSelf" provider/
```

Note: `provider/process_test.go` contains `TestProcessSearcher_SkipsSelf` (commit `82025a9`). This test must still pass.

- [ ] **Step 2.2: Rewrite `process_darwin.go` to use shared cache**

Replace contents of `k2/provider/process_darwin.go`:

```go
//go:build darwin && !ios

package provider

import (
    "fmt"
    "net/netip"
    "os"
    "os/exec"
    "strconv"
    "strings"
)

// DarwinProcessSearcher uses lsof to look up process names by source (IP, port).
// Results cached via shared processCache helper.
type DarwinProcessSearcher struct {
    cache *processCache
}

// NewDarwinProcessSearcher constructs a Darwin process searcher with the
// project-standard cache (2s TTL, 4096 soft cap).
func NewDarwinProcessSearcher() *DarwinProcessSearcher {
    return &DarwinProcessSearcher{
        cache: newProcessCache(processCacheCap, processCacheTTL),
    }
}

func (s *DarwinProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
    if !srcIP.Is4() {
        return "", ""
    }
    // Lazy-init cache for zero-value struct callers (e.g. daemon factory
    // pre-shared-cache callers that did `&DarwinProcessSearcher{}`).
    if s.cache == nil {
        s.cache = newProcessCache(processCacheCap, processCacheTTL)
    }
    key := cacheKey(network, srcIP, srcPort)
    if name, pkg, ok := s.cache.get(key); ok {
        return name, pkg
    }
    name := lsofLookup(network, srcIP, srcPort)
    s.cache.set(key, name, "")
    return name, ""
}

// lsofLookup calls lsof to find the process name for a socket.
// lsof -i <proto>@<ip>:<port> returns BOTH endpoints (client + server) when
// a connection is established. We must exclude our own PID (the daemon) so
// we return the actual peer (e.g., curl, browser).
//
// Output format (-F pc): one "p<pid>" line per process, followed by a "c<command>"
// line, then "f<fd>" lines per matching socket. We scan line-by-line, tracking
// the current PID, and return the first command whose PID is not our own.
func lsofLookup(network string, srcIP netip.Addr, srcPort uint16) string {
    proto := "tcp"
    if network == "udp" {
        proto = "udp"
    }
    spec := fmt.Sprintf("%s@%s:%d", proto, srcIP, srcPort)

    out, err := exec.Command("lsof", "-i", spec, "-P", "-n", "-F", "pc").Output()
    if err != nil || len(out) == 0 {
        return ""
    }

    selfPid := os.Getpid()
    var currentPid int
    var currentCmd string
    for _, line := range strings.Split(string(out), "\n") {
        if len(line) < 1 {
            continue
        }
        switch line[0] {
        case 'p':
            if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
                return currentCmd
            }
            pid, err := strconv.Atoi(line[1:])
            if err != nil {
                currentPid = 0
                currentCmd = ""
                continue
            }
            currentPid = pid
            currentCmd = ""
        case 'c':
            currentCmd = line[1:]
        }
    }
    if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
        return currentCmd
    }
    return ""
}
```

- [ ] **Step 2.3: Update Darwin daemon factory to use constructor**

Read `k2/daemon/process_darwin.go`:

```bash
cat k2/daemon/process_darwin.go
```

Current contents return `&provider.DarwinProcessSearcher{}`. Update to use `NewDarwinProcessSearcher()`:

```go
//go:build darwin && !ios

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns a Darwin-specific ProcessSearcher using lsof
// to look up the process owning each connection by (network, srcIP, srcPort).
func newProcessSearcher() provider.ProcessSearcher {
    return provider.NewDarwinProcessSearcher()
}
```

- [ ] **Step 2.4: Run Darwin tests, verify existing tests pass**

```bash
go test ./provider/ -run TestProcessSearcher -v
go test ./daemon/ -run TestProcess -v
```

Expected: existing `TestProcessSearcher_SkipsSelf` still PASS. No new test failures.

- [ ] **Step 2.5: Run full provider test suite**

```bash
go test ./provider/ -v
```

Expected: ALL PASS (cache tests from Task 1 + existing Darwin tests).

- [ ] **Step 2.6: Commit**

```bash
git -C k2 add provider/process_darwin.go daemon/process_darwin.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "refactor(provider): Darwin uses shared processCache" \
  -m "DarwinProcessSearcher switches from private sync.Mutex+map to shared
*processCache. Adds IPv4-only early return for consistency with Linux/Windows.
Existing self-PID skip behavior preserved (lsofLookup unchanged)."
```

---

## Phase 2 — Linux Desktop

### Task 3: Linux refactor — procRoot + shared cache + IPv4 + self-PID + DIAG

**Files:**
- Modify: `k2/provider/process_linux.go`
- Modify: `k2/provider/process_linux_test.go`

This task lifts the bulk of changes WITHOUT adding the inode reverse index. The old `findPIDByInode` full walk is kept; index is added in Task 4. This isolates the structural refactor from the perf optimization.

- [ ] **Step 3.1: Read current `process_linux.go` for reference**

```bash
cat k2/provider/process_linux.go
```

Confirm current signature: `func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher` — must preserve.

- [ ] **Step 3.2: Write failing test that uses procRoot injection**

Replace `k2/provider/process_linux_test.go` contents:

```go
//go:build linux

package provider

import (
    "net/netip"
    "os"
    "path/filepath"
    "testing"
)

type mockPackageResolver struct {
    packages map[int32]string
}

func (m *mockPackageResolver) PackageForUID(uid int32) string {
    return m.packages[uid]
}

func TestLinuxProcessSearcher_PreservesResolverSignature(t *testing.T) {
    mock := &mockPackageResolver{packages: map[int32]string{
        10156: "com.android.chrome",
    }}
    searcher := NewLinuxProcessSearcher(mock)
    if searcher.packageResolver == nil {
        t.Error("packageResolver should be set")
    }
}

// setupFakeProcfs builds a minimal /proc tree:
//   /proc/net/tcp: socket inode 1001 owned by 192.168.1.5:54321 (state 01)
//   /proc/8000/fd/3 → socket:[1001]
//   /proc/8000/comm: "chrome\n"
//   /proc/8000/status: includes "Uid:\t10156\t..."
func setupFakeProcfs(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    must := func(err error) {
        t.Helper()
        if err != nil {
            t.Fatalf("setup: %v", err)
        }
    }
    must(os.MkdirAll(filepath.Join(dir, "net"), 0755))
    // /proc/net/tcp line: local_address 0501A8C0:D431 = 192.168.1.5:54321
    //   (LE uint32 0x0501A8C0 written as hex chars in proc format)
    tcpLine := "  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode\n" +
        "   0: 0501A8C0:D431 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1001 0 0000000000000000 100 0 0 10 0\n"
    must(os.WriteFile(filepath.Join(dir, "net/tcp"), []byte(tcpLine), 0644))
    must(os.MkdirAll(filepath.Join(dir, "8000/fd"), 0755))
    must(os.Symlink("socket:[1001]", filepath.Join(dir, "8000/fd/3")))
    must(os.WriteFile(filepath.Join(dir, "8000/comm"), []byte("chrome\n"), 0644))
    must(os.WriteFile(filepath.Join(dir, "8000/status"),
        []byte("Name:\tchrome\nUid:\t10156\t10156\t10156\t10156\n"), 0644))
    return dir
}

func TestFindProcess_IPv6Early(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = "/nonexistent"  // would fail if reached
    name, pkg := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54321)
    if name != "" || pkg != "" {
        t.Errorf("got (%q,%q), want empty", name, pkg)
    }
}

func TestFindProcess_FakeProcfs_TCPMatch(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "chrome" {
        t.Errorf("name = %q, want chrome", name)
    }
}

func TestFindProcess_CacheHit_NoProcfsRescan(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    // First lookup populates cache.
    s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    // Now delete procfs — second lookup must hit cache, not procfs.
    if err := os.RemoveAll(s.procRoot); err != nil {
        t.Fatal(err)
    }
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "chrome" {
        t.Errorf("expected cache hit returning chrome, got %q", name)
    }
}

func TestFindProcess_NoMatch_NegativeCache(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    // Port that doesn't exist in fake procfs.
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 9999)
    if name != "" {
        t.Errorf("got %q, want empty", name)
    }
}

func TestFindProcess_PackageResolution(t *testing.T) {
    mock := &mockPackageResolver{packages: map[int32]string{
        10156: "com.android.chrome",
    }}
    s := NewLinuxProcessSearcher(mock)
    s.procRoot = setupFakeProcfs(t)
    name, pkg := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "chrome" || pkg != "com.android.chrome" {
        t.Errorf("got (%q,%q), want (chrome, com.android.chrome)", name, pkg)
    }
}
```

- [ ] **Step 3.3: Run tests, verify they fail**

```bash
go test ./provider/ -run TestFindProcess -tags linux -v
```

Expected: FAIL (procRoot field missing, FindProcess signature unchanged but field access fails).

- [ ] **Step 3.4: Rewrite `process_linux.go`**

Replace `k2/provider/process_linux.go`:

```go
//go:build linux

package provider

import (
    "bufio"
    "encoding/hex"
    "fmt"
    "log/slog"
    "net/netip"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

// LinuxProcessSearcher looks up process names by reading /proc/net/tcp{,6}
// to find the socket inode, then scanning /proc/*/fd/ for the owning PID,
// and reading /proc/PID/comm for the process name.
//
// Note: Task 3 keeps the O(N) /proc/*/fd walk per lookup. Task 4 adds an
// inode→PID reverse index with stale-while-revalidate to amortize this cost.
type LinuxProcessSearcher struct {
    packageResolver PackageResolver
    cache           *processCache
    procRoot        string  // default "/proc", overridable for tests
}

// NewLinuxProcessSearcher creates a LinuxProcessSearcher with an optional
// PackageResolver for Android UID→package name mapping.
// Pass nil on non-Android Linux.
func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher {
    return &LinuxProcessSearcher{
        packageResolver: pr,
        cache:           newProcessCache(processCacheCap, processCacheTTL),
        procRoot:        "/proc",
    }
}

func (s *LinuxProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
    if !srcIP.Is4() {
        return "", ""
    }
    key := cacheKey(network, srcIP, srcPort)
    if name, pkg, ok := s.cache.get(key); ok {
        return name, pkg
    }

    start := time.Now()
    name, pkg := s.lookupUncached(network, srcIP, srcPort)
    if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
        slog.Info("DIAG: process-lookup-slow",
            "platform", "linux",
            "network", network,
            "latencyMs", elapsed.Milliseconds())
    }

    s.cache.set(key, name, pkg)
    return name, pkg
}

func (s *LinuxProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
    inode := s.findSocketInode(network, srcIP, srcPort)
    if inode == 0 {
        return "", ""
    }
    pid := s.findPIDByInode(inode)
    if pid == 0 || pid == os.Getpid() {
        return "", ""
    }
    name := s.readProcessName(pid)

    var pkg string
    if s.packageResolver != nil {
        if uid := s.readProcessUID(pid); uid >= 10000 {
            pkg = s.packageResolver.PackageForUID(int32(uid))
        }
    }
    return name, pkg
}

func (s *LinuxProcessSearcher) findSocketInode(network string, srcIP netip.Addr, srcPort uint16) uint64 {
    var path string
    switch {
    case network == "udp" && srcIP.Is4():
        path = filepath.Join(s.procRoot, "net/udp")
    case network == "udp":
        path = filepath.Join(s.procRoot, "net/udp6")
    case srcIP.Is4():
        path = filepath.Join(s.procRoot, "net/tcp")
    default:
        path = filepath.Join(s.procRoot, "net/tcp6")
    }
    return searchProcNet(path, srcIP, srcPort)
}

// searchProcNet parses a /proc/net/tcp{,6} or /proc/net/udp{,6} file.
// Format: sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode ...
func searchProcNet(path string, srcIP netip.Addr, srcPort uint16) uint64 {
    f, err := os.Open(path)
    if err != nil {
        return 0
    }
    defer f.Close()

    scanner := bufio.NewScanner(f)
    scanner.Scan() // skip header line
    for scanner.Scan() {
        fields := strings.Fields(scanner.Text())
        if len(fields) < 10 {
            continue
        }
        ip, port, ok := parseProcNetAddr(fields[1])
        if !ok || port != srcPort {
            continue
        }
        if ip != srcIP {
            continue
        }
        inode, err := strconv.ParseUint(fields[9], 10, 64)
        if err != nil {
            continue
        }
        return inode
    }
    return 0
}

// parseProcNetAddr parses "0100007F:0035" or IPv6 hex format from /proc/net/tcp.
func parseProcNetAddr(s string) (netip.Addr, uint16, bool) {
    parts := strings.SplitN(s, ":", 2)
    if len(parts) != 2 {
        return netip.Addr{}, 0, false
    }
    port, err := strconv.ParseUint(parts[1], 16, 16)
    if err != nil {
        return netip.Addr{}, 0, false
    }
    hexIP := parts[0]
    ipBytes, err := hex.DecodeString(hexIP)
    if err != nil {
        return netip.Addr{}, 0, false
    }
    var addr netip.Addr
    switch len(ipBytes) {
    case 4:
        var b [4]byte
        b[0], b[1], b[2], b[3] = ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]
        addr = netip.AddrFrom4(b)
    case 16:
        var b [16]byte
        for i := 0; i < 4; i++ {
            off := i * 4
            b[off], b[off+1], b[off+2], b[off+3] = ipBytes[off+3], ipBytes[off+2], ipBytes[off+1], ipBytes[off]
        }
        addr = netip.AddrFrom16(b)
    default:
        return netip.Addr{}, 0, false
    }
    return addr, uint16(port), true
}

// findPIDByInode scans s.procRoot/*/fd/ for a symlink pointing to the socket inode.
// O(N) walk. Replaced in Task 4 with cached reverse index.
func (s *LinuxProcessSearcher) findPIDByInode(inode uint64) int {
    target := fmt.Sprintf("socket:[%d]", inode)
    entries, err := os.ReadDir(s.procRoot)
    if err != nil {
        return 0
    }
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        pid, err := strconv.Atoi(entry.Name())
        if err != nil {
            continue
        }
        fdDir := filepath.Join(s.procRoot, entry.Name(), "fd")
        fds, err := os.ReadDir(fdDir)
        if err != nil {
            continue
        }
        for _, fd := range fds {
            link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
            if err != nil {
                continue
            }
            if link == target {
                return pid
            }
        }
    }
    return 0
}

func (s *LinuxProcessSearcher) readProcessName(pid int) string {
    data, err := os.ReadFile(filepath.Join(s.procRoot, strconv.Itoa(pid), "comm"))
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(data))
}

func (s *LinuxProcessSearcher) readProcessUID(pid int) int {
    f, err := os.Open(filepath.Join(s.procRoot, strconv.Itoa(pid), "status"))
    if err != nil {
        return -1
    }
    defer f.Close()
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "Uid:") {
            fields := strings.Fields(line)
            if len(fields) >= 2 {
                uid, _ := strconv.Atoi(fields[1])
                return uid
            }
        }
    }
    return -1
}
```

- [ ] **Step 3.5: Run tests, verify all pass**

```bash
go test ./provider/ -v
```

Expected: all 11 tests PASS (5 cache, 6 linux from Task 3).

- [ ] **Step 3.6: Commit**

```bash
git -C k2 add provider/process_linux.go provider/process_linux_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "refactor(provider): Linux ProcessSearcher uses shared cache + procRoot injection" \
  -m "Adds IPv4 early return, self-PID defensive skip, DIAG process-lookup-slow
timing wrapper. procRoot field allows unit tests to inject a fake /proc tree.
NewLinuxProcessSearcher(pr) signature preserved → Android appext zero change.
Inode→PID reverse index optimization deferred to next commit."
```

---

### Task 4: Linux inode reverse index — stale-while-revalidate + singleflight

**Files:**
- Modify: `k2/provider/process_linux.go`
- Modify: `k2/provider/process_linux_test.go`

- [ ] **Step 4.1: Check `golang.org/x/sync/singleflight` already in go.mod**

```bash
go list -m golang.org/x/sync 2>&1
```

Expected: already in (k2 transitively depends on it). If not, the implementation step will `go get` it.

- [ ] **Step 4.2: Write failing tests for index behavior**

Append to `k2/provider/process_linux_test.go`. The existing import block from Task 3 has `net/netip`, `os`, `path/filepath`, `testing`. Add these to the import block (merge or add new block — Go accepts both):

```go
import (
    "strconv"
    "sync"
    "sync/atomic"
    "time"
)

func TestLookupPID_ColdStartBuildsIndex(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    pid := s.lookupPID(1001)
    if pid != 8000 {
        t.Errorf("pid = %d, want 8000", pid)
    }
}

func TestLookupPID_StaleReturnsCachedAsyncRebuilds(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    // Cold start: builds index synchronously.
    if pid := s.lookupPID(1001); pid != 8000 {
        t.Fatalf("cold start pid = %d, want 8000", pid)
    }
    // Force stale by rewinding expires.
    s.indexMu.Lock()
    s.indexExpires = time.Now().Add(-time.Second)
    s.indexMu.Unlock()
    // Stale call must return cached value immediately.
    if pid := s.lookupPID(1001); pid != 8000 {
        t.Errorf("stale pid = %d, want 8000 (stale cached)", pid)
    }
    // Wait for async rebuild to complete.
    for i := 0; i < 100; i++ {
        s.indexMu.RLock()
        fresh := time.Now().Before(s.indexExpires)
        s.indexMu.RUnlock()
        if fresh {
            return
        }
        time.Sleep(10 * time.Millisecond)
    }
    t.Errorf("async rebuild did not complete within 1s")
}

func TestLookupPID_SingleflightDedupsRebuild(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    s.procRoot = setupFakeProcfs(t)
    // Hook buildInodeIndex via counter — wrap with a slow build that increments
    // before delegating, to maximize concurrent overlap.
    var calls atomic.Int32
    origBuild := s.buildInodeIndexFn
    s.buildInodeIndexFn = func() map[uint64]int {
        calls.Add(1)
        time.Sleep(50 * time.Millisecond)  // make overlap likely
        return origBuild()
    }
    // 10 concurrent cold-start lookups → singleflight should collapse to 1 build.
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            s.lookupPID(1001)
        }()
    }
    wg.Wait()
    if got := calls.Load(); got != 1 {
        t.Errorf("buildInodeIndex called %d times, want 1 (singleflight)", got)
    }
}

func TestSelfPIDSkip_ViaFakeProcfs(t *testing.T) {
    s := NewLinuxProcessSearcher(nil)
    dir := t.TempDir()
    // Build minimal /proc with our OWN pid owning the inode.
    selfPid := os.Getpid()
    selfDir := filepath.Join(dir, strconv.Itoa(selfPid))
    if err := os.MkdirAll(filepath.Join(selfDir, "fd"), 0755); err != nil {
        t.Fatal(err)
    }
    if err := os.Symlink("socket:[1001]", filepath.Join(selfDir, "fd/3")); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(selfDir, "comm"), []byte("k2-test\n"), 0644); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(filepath.Join(dir, "net"), 0755); err != nil {
        t.Fatal(err)
    }
    tcpLine := "  sl  local_address rem_address\n" +
        "   0: 0501A8C0:D431 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1001 0 0000000000000000 100 0 0 10 0\n"
    if err := os.WriteFile(filepath.Join(dir, "net/tcp"), []byte(tcpLine), 0644); err != nil {
        t.Fatal(err)
    }
    s.procRoot = dir
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "" {
        t.Errorf("expected self-PID skip → empty name, got %q", name)
    }
}
```

The test references `s.buildInodeIndexFn` — this is the seam we'll add for testability.

- [ ] **Step 4.3: Run tests, verify they fail**

```bash
go test ./provider/ -run TestLookupPID -tags linux -v
go test ./provider/ -run TestSelfPIDSkip -tags linux -v
```

Expected: FAIL with "undefined: lookupPID", "undefined: buildInodeIndexFn", "undefined: indexMu".

- [ ] **Step 4.4: Modify `process_linux.go` to add index + stale-while-revalidate**

Add imports and types at the top (after existing imports):

```go
import (
    // ... existing imports ...
    "github.com/sasha-s/go-deadlock"
    "github.com/kaitu-io/k2/safego"
    "golang.org/x/sync/singleflight"
)

const inodeIndexTTL = 5 * time.Second
```

Replace `LinuxProcessSearcher` struct (keeping existing exported fields, adding new):

```go
type LinuxProcessSearcher struct {
    packageResolver PackageResolver
    cache           *processCache
    procRoot        string

    indexMu      deadlock.RWMutex
    inodeIndex   map[uint64]int  // inode → PID snapshot
    indexExpires time.Time       // zero = never built

    sf                singleflight.Group  // dedup concurrent rebuilds
    buildInodeIndexFn func() map[uint64]int  // seam for tests
}
```

Replace `NewLinuxProcessSearcher`:

```go
func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher {
    s := &LinuxProcessSearcher{
        packageResolver: pr,
        cache:           newProcessCache(processCacheCap, processCacheTTL),
        procRoot:        "/proc",
    }
    s.buildInodeIndexFn = s.buildInodeIndex
    return s
}
```

Replace `lookupUncached` to call new `lookupPID`:

```go
func (s *LinuxProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
    inode := s.findSocketInode(network, srcIP, srcPort)
    if inode == 0 {
        return "", ""
    }
    pid := s.lookupPID(inode)
    if pid == 0 || pid == os.Getpid() {
        return "", ""
    }
    name := s.readProcessName(pid)

    var pkg string
    if s.packageResolver != nil {
        if uid := s.readProcessUID(pid); uid >= 10000 {
            pkg = s.packageResolver.PackageForUID(int32(uid))
        }
    }
    return name, pkg
}
```

Add `lookupPID`, `doRebuild`, `buildInodeIndex`. Delete old `findPIDByInode` from Task 3:

```go
// lookupPID resolves an inode → PID via the cached reverse index.
// Cold start: synchronous index build (singleflight-protected).
// Stale: returns last-known value, kicks async rebuild.
// Fresh: direct map lookup.
func (s *LinuxProcessSearcher) lookupPID(inode uint64) int {
    s.indexMu.RLock()
    indexBuilt := !s.indexExpires.IsZero()
    pid := s.inodeIndex[inode]
    expired := indexBuilt && time.Now().After(s.indexExpires)
    s.indexMu.RUnlock()

    if !indexBuilt {
        s.sf.Do("rebuild", s.doRebuild)
        s.indexMu.RLock()
        pid = s.inodeIndex[inode]
        s.indexMu.RUnlock()
        return pid
    }

    if expired {
        safego.Go(func() { s.sf.Do("rebuild", s.doRebuild) })
    }
    return pid
}

func (s *LinuxProcessSearcher) doRebuild() (any, error) {
    idx := s.buildInodeIndexFn()
    s.indexMu.Lock()
    s.inodeIndex = idx
    s.indexExpires = time.Now().Add(inodeIndexTTL)
    s.indexMu.Unlock()
    return nil, nil
}

// buildInodeIndex walks s.procRoot/*/fd/ once and returns inode→PID map.
// Replaces per-lookup O(N) walk with single-pass index. Singleflight
// (via doRebuild) ensures concurrent triggers collapse to one build.
func (s *LinuxProcessSearcher) buildInodeIndex() map[uint64]int {
    idx := make(map[uint64]int, 1024)
    entries, err := os.ReadDir(s.procRoot)
    if err != nil {
        return idx
    }
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        pid, err := strconv.Atoi(entry.Name())
        if err != nil {
            continue
        }
        fdDir := filepath.Join(s.procRoot, entry.Name(), "fd")
        fds, err := os.ReadDir(fdDir)
        if err != nil {
            continue
        }
        for _, fd := range fds {
            link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
            if err != nil || !strings.HasPrefix(link, "socket:[") {
                continue
            }
            end := strings.IndexByte(link, ']')
            if end < 0 {
                continue
            }
            inode, err := strconv.ParseUint(link[8:end], 10, 64)
            if err != nil {
                continue
            }
            idx[inode] = pid
        }
    }
    return idx
}
```

Remove the old `findPIDByInode` method from Task 3 — replaced by `lookupPID` + `buildInodeIndex`.

- [ ] **Step 4.5: Run tests, verify all pass**

```bash
go test ./provider/ -v
```

Expected: all 15 tests PASS.

- [ ] **Step 4.6: Run with race detector**

```bash
go test -race ./provider/ -timeout 60s
```

Expected: PASS, no race conditions.

- [ ] **Step 4.7: Commit**

```bash
git -C k2 add provider/process_linux.go provider/process_linux_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "perf(provider): Linux inode→PID reverse index with stale-while-revalidate" \
  -m "Replaces per-lookup O(N) walk of /proc/*/fd with cached inode→PID map
(5s TTL). Cold start: synchronous build (~50-300ms one-time). Stale calls
return cached value, kick async rebuild via safego.Go. Singleflight collapses
concurrent rebuild triggers to one walk.

Risk: stale-period PID recycling (~0 probability on desktop given PID_MAX
32768 and ~6500/s spawn floor)."
```

---

## Phase 3 — Windows

### Task 5: Windows byte-order helpers + struct types

**Files:**
- Create: `k2/provider/process_windows.go` (initial — types + helpers only)
- Create: `k2/provider/process_windows_test.go`

These can be partially tested cross-platform via Go's build tag handling: tests gated to `//go:build windows` won't run on macOS dev box, but `GOOS=windows go vet` validates. Real test execution requires Windows.

- [ ] **Step 5.1: Write failing test file**

Create `k2/provider/process_windows_test.go`:

```go
//go:build windows

package provider

import "testing"

func TestNtohs_KnownValues(t *testing.T) {
    cases := []struct {
        in   uint32
        want uint16
    }{
        {0x000031D4, 54321},  // 0xD431 in NBO
        {0x00005000, 80},     // 0x0050 in NBO
        {0x0000BB01, 443},    // 0x01BB in NBO
        {0x00000000, 0},
        {0x0000FFFF, 0xFFFF},
    }
    for _, c := range cases {
        got := ntohs(c.in)
        if got != c.want {
            t.Errorf("ntohs(0x%08X) = %d, want %d", c.in, got, c.want)
        }
    }
}

func TestUint32ToIPv4_KnownValues(t *testing.T) {
    cases := []struct {
        in   uint32
        want [4]byte
    }{
        {0x0501A8C0, [4]byte{192, 168, 1, 5}},
        {0x0100007F, [4]byte{127, 0, 0, 1}},
        {0x00000000, [4]byte{0, 0, 0, 0}},
        {0x010101D5, [4]byte{213, 1, 1, 1}},  // 213.1.1.1 ← 0xD5010101 NBO
    }
    for _, c := range cases {
        got := uint32ToIPv4(c.in)
        if got != c.want {
            t.Errorf("uint32ToIPv4(0x%08X) = %v, want %v", c.in, got, c.want)
        }
    }
}
```

- [ ] **Step 5.2: Verify test fails on Windows toolchain (cross-vet)**

```bash
GOOS=windows go vet ./provider/
```

Expected: VET FAIL with "undefined: ntohs", "undefined: uint32ToIPv4".

- [ ] **Step 5.3: Create `process_windows.go` with helpers and types only**

Create `k2/provider/process_windows.go`:

```go
//go:build windows

package provider

import (
    "encoding/binary"
)

// tcpRowOwnerPID mirrors MIB_TCPROW_OWNER_PID (iphlpapi.h).
// Fields stored in network byte order; use ntohs / uint32ToIPv4 to decode.
type tcpRowOwnerPID struct {
    State      uint32
    LocalAddr  uint32
    LocalPort  uint32
    RemoteAddr uint32
    RemotePort uint32
    OwningPID  uint32
}

// udpRowOwnerPID mirrors MIB_UDPROW_OWNER_PID. No remote endpoint.
type udpRowOwnerPID struct {
    LocalAddr uint32
    LocalPort uint32
    OwningPID uint32
}

// Constants from iphlpapi.h.
const (
    AF_INET                 = 2
    TCP_TABLE_OWNER_PID_ALL = 5
    UDP_TABLE_OWNER_PID     = 1
)

// ntohs converts the low 16 bits of n from network byte order to host order.
//
// Windows TCP/UDP table fields (dwLocalPort, dwRemotePort) store the port
// in the low 16 bits of a uint32, in network byte order. Example: port
// 54321 (0xD431 NBO bytes [D4,31]) is stored as low 16 bits of dwLocalPort
// = 0x31D4. ntohs(0x31D4) = 0xD431 = 54321.
func ntohs(n uint32) uint16 {
    v := uint16(n & 0xFFFF)
    return (v >> 8) | (v << 8)
}

// uint32ToIPv4 extracts 4 IPv4 bytes from a uint32 stored in Windows
// network byte order. Example: dwLocalAddr 0x0501A8C0 (LE uint32) means
// IP 192.168.1.5 — PutUint32(LittleEndian) writes [0xC0, 0xA8, 0x01, 0x05].
func uint32ToIPv4(n uint32) [4]byte {
    var b [4]byte
    binary.LittleEndian.PutUint32(b[:], n)
    return b
}
```

- [ ] **Step 5.4: Verify cross-compile + vet pass**

```bash
GOOS=windows go vet ./provider/
GOOS=windows go build ./provider/
```

Expected: both PASS.

- [ ] **Step 5.5: Commit**

```bash
git -C k2 add provider/process_windows.go provider/process_windows_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "feat(provider): Windows byte-order helpers + MIB_TCPROW types" \
  -m "Foundation for WindowsProcessSearcher. ntohs handles dwLocalPort low
16 bits NBO; uint32ToIPv4 handles dwLocalAddr 4-byte NBO. Concrete-value
unit tests pin both functions."
```

---

### Task 6: Windows real syscall fetcher + process namer

**Files:**
- Modify: `k2/provider/process_windows.go`

This task adds the real syscall wrappers. They can't be unit-tested without running on Windows; verification is `GOOS=windows go build` + manual smoke test in Phase 7.

- [ ] **Step 6.1: Verify `golang.org/x/sys/windows` already in go.mod**

```bash
go list -m golang.org/x/sys 2>&1
```

Expected: present (k2 already uses it elsewhere).

- [ ] **Step 6.2: Append fetcher + namer to `process_windows.go`**

Append to `k2/provider/process_windows.go`:

```go
import (
    "fmt"
    "path/filepath"
    "unsafe"

    "golang.org/x/sys/windows"
)

var (
    iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
    procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
    procGetExtendedUdpTable = iphlpapi.NewProc("GetExtendedUdpTable")
)

// tcpTableFetcher is the seam between the searcher's matching logic and the
// underlying iphlpapi syscall. Tests inject mocks.
type tcpTableFetcher interface {
    Fetch() ([]tcpRowOwnerPID, error)
}

type udpTableFetcher interface {
    Fetch() ([]udpRowOwnerPID, error)
}

type processNamer interface {
    NameForPID(pid uint32) string
}

type realTCPFetcher struct{}

func (realTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
    var size uint32
    // First call: probe for required buffer size.
    procGetExtendedTcpTable.Call(
        0, uintptr(unsafe.Pointer(&size)),
        0, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0,
    )
    if size == 0 {
        return nil, nil
    }
    buf := make([]byte, size)
    ret, _, _ := procGetExtendedTcpTable.Call(
        uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
        0, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0,
    )
    if ret != 0 {
        return nil, fmt.Errorf("GetExtendedTcpTable: %d", ret)
    }
    // buf layout: [uint32 dwNumEntries] [tcpRowOwnerPID × N]
    n := *(*uint32)(unsafe.Pointer(&buf[0]))
    if n == 0 {
        return nil, nil
    }
    raw := unsafe.Slice(
        (*tcpRowOwnerPID)(unsafe.Pointer(&buf[4])),
        int(n),
    )
    out := make([]tcpRowOwnerPID, n)
    copy(out, raw)  // copy out of buf — buf is GC'd after return
    return out, nil
}

type realUDPFetcher struct{}

func (realUDPFetcher) Fetch() ([]udpRowOwnerPID, error) {
    var size uint32
    procGetExtendedUdpTable.Call(
        0, uintptr(unsafe.Pointer(&size)),
        0, AF_INET, UDP_TABLE_OWNER_PID, 0,
    )
    if size == 0 {
        return nil, nil
    }
    buf := make([]byte, size)
    ret, _, _ := procGetExtendedUdpTable.Call(
        uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
        0, AF_INET, UDP_TABLE_OWNER_PID, 0,
    )
    if ret != 0 {
        return nil, fmt.Errorf("GetExtendedUdpTable: %d", ret)
    }
    n := *(*uint32)(unsafe.Pointer(&buf[0]))
    if n == 0 {
        return nil, nil
    }
    raw := unsafe.Slice(
        (*udpRowOwnerPID)(unsafe.Pointer(&buf[4])),
        int(n),
    )
    out := make([]udpRowOwnerPID, n)
    copy(out, raw)
    return out, nil
}

type realProcNamer struct{}

func (realProcNamer) NameForPID(pid uint32) string {
    // PROCESS_QUERY_LIMITED_INFORMATION works on system-protected processes
    // (smss/csrss/etc.) where PROCESS_QUERY_INFORMATION would be denied.
    h, err := windows.OpenProcess(
        windows.PROCESS_QUERY_LIMITED_INFORMATION,
        false, pid,
    )
    if err != nil {
        return ""
    }
    defer windows.CloseHandle(h)

    var size uint32 = windows.MAX_PATH
    buf := make([]uint16, size)
    if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
        return ""
    }
    return filepath.Base(windows.UTF16ToString(buf[:size]))
}
```

- [ ] **Step 6.3: Cross-compile validation**

```bash
GOOS=windows go vet ./provider/
GOOS=windows go build ./provider/
```

Expected: both PASS.

- [ ] **Step 6.4: Commit**

```bash
git -C k2 add provider/process_windows.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "feat(provider): Windows real syscall fetcher + process namer" \
  -m "realTCPFetcher / realUDPFetcher call GetExtendedTcpTable /
GetExtendedUdpTable via LazyDLL syscall (pure Go, no CGo). realProcNamer
uses OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION) + QueryFullProcessImageName.
Interface seams (tcpTableFetcher / udpTableFetcher / processNamer) enable
mock injection for unit tests."
```

---

### Task 7: Windows `WindowsProcessSearcher` with mock-fetcher tests

**Files:**
- Modify: `k2/provider/process_windows.go`
- Modify: `k2/provider/process_windows_test.go`

- [ ] **Step 7.1: Write failing tests**

Append to `k2/provider/process_windows_test.go`:

```go
import (
    "net/netip"
    "os"
    "sync/atomic"
)

type mockTCPFetcher struct {
    rows  []tcpRowOwnerPID
    err   error
    calls atomic.Int32
}

func (m *mockTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
    m.calls.Add(1)
    return m.rows, m.err
}

type mockUDPFetcher struct {
    rows  []udpRowOwnerPID
    err   error
    calls atomic.Int32
}

func (m *mockUDPFetcher) Fetch() ([]udpRowOwnerPID, error) {
    m.calls.Add(1)
    return m.rows, m.err
}

type mockProcNamer struct {
    names map[uint32]string
}

func (m *mockProcNamer) NameForPID(pid uint32) string {
    return m.names[pid]
}

func newTestSearcher(tcp *mockTCPFetcher, udp *mockUDPFetcher, namer *mockProcNamer) *WindowsProcessSearcher {
    return &WindowsProcessSearcher{
        cache:      newProcessCache(processCacheCap, processCacheTTL),
        tcpFetcher: tcp,
        udpFetcher: udp,
        procNamer:  namer,
        selfPID:    uint32(os.Getpid()),
    }
}

// 192.168.1.5:54321 → app PID 8000
//   dwLocalAddr = 0x0501A8C0 (LE uint32 of NBO [C0,A8,01,05])
//   dwLocalPort low 16 = 0x31D4 (NBO of 0xD431)
var chromeRow = tcpRowOwnerPID{
    State:     5,
    LocalAddr: 0x0501A8C0,
    LocalPort: 0x000031D4,
    OwningPID: 8000,
}

func TestWindowsFindProcess_IPv6Early(t *testing.T) {
    tcp := &mockTCPFetcher{}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54321)
    if name != "" {
        t.Errorf("got %q, want empty", name)
    }
    if tcp.calls.Load() != 0 {
        t.Errorf("fetcher called %d times for IPv6, want 0", tcp.calls.Load())
    }
}

func TestWindowsFindProcess_TCPMatch(t *testing.T) {
    tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
    namer := &mockProcNamer{names: map[uint32]string{8000: "chrome.exe"}}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "chrome.exe" {
        t.Errorf("got %q, want chrome.exe", name)
    }
}

func TestWindowsFindProcess_PIDZeroSkipped(t *testing.T) {
    row := chromeRow
    row.OwningPID = 0  // TIME_WAIT zombie
    tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{row}}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{}})
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "" {
        t.Errorf("got %q, want empty (PID=0 should be skipped)", name)
    }
}

func TestWindowsFindProcess_SelfPIDSkipped(t *testing.T) {
    row := chromeRow
    row.OwningPID = uint32(os.Getpid())
    tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{row}}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{
        uint32(os.Getpid()): "k2.exe",
    }})
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "" {
        t.Errorf("got %q, want empty (self-PID should be skipped)", name)
    }
}

func TestWindowsFindProcess_CacheHit(t *testing.T) {
    tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
    namer := &mockProcNamer{names: map[uint32]string{8000: "chrome.exe"}}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
    addr := netip.MustParseAddr("192.168.1.5")
    s.FindProcess("tcp", addr, 54321)
    s.FindProcess("tcp", addr, 54321)  // should hit cache
    if tcp.calls.Load() != 1 {
        t.Errorf("fetcher called %d times, want 1 (cache hit on second call)", tcp.calls.Load())
    }
}

func TestWindowsFindProcess_NegativeCache(t *testing.T) {
    tcp := &mockTCPFetcher{rows: nil}  // empty table
    s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
    addr := netip.MustParseAddr("192.168.1.5")
    s.FindProcess("tcp", addr, 54321)
    s.FindProcess("tcp", addr, 54321)  // negative cache hit
    if tcp.calls.Load() != 1 {
        t.Errorf("fetcher called %d times, want 1 (negative cache hit)", tcp.calls.Load())
    }
}

func TestWindowsFindProcess_NoMatchEmptyTable(t *testing.T) {
    tcp := &mockTCPFetcher{rows: nil}
    s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "" {
        t.Errorf("got %q, want empty", name)
    }
}

func TestWindowsFindProcess_UDPMatch(t *testing.T) {
    udp := &mockUDPFetcher{rows: []udpRowOwnerPID{{
        LocalAddr: 0x0501A8C0,
        LocalPort: 0x000031D4,
        OwningPID: 9000,
    }}}
    namer := &mockProcNamer{names: map[uint32]string{9000: "dns-app.exe"}}
    s := newTestSearcher(&mockTCPFetcher{}, udp, namer)
    name, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54321)
    if name != "dns-app.exe" {
        t.Errorf("got %q, want dns-app.exe", name)
    }
}
```

- [ ] **Step 7.2: Verify cross-vet fails (struct fields not yet defined)**

```bash
GOOS=windows go vet ./provider/
```

Expected: FAIL with "undefined: WindowsProcessSearcher".

- [ ] **Step 7.3: Append `WindowsProcessSearcher` to `process_windows.go`**

Append to `k2/provider/process_windows.go`:

```go
import (
    "log/slog"
    "net/netip"
    "os"
    "time"
)

type WindowsProcessSearcher struct {
    cache      *processCache
    tcpFetcher tcpTableFetcher
    udpFetcher udpTableFetcher
    procNamer  processNamer
    selfPID    uint32
}

func NewWindowsProcessSearcher() *WindowsProcessSearcher {
    return &WindowsProcessSearcher{
        cache:      newProcessCache(processCacheCap, processCacheTTL),
        tcpFetcher: realTCPFetcher{},
        udpFetcher: realUDPFetcher{},
        procNamer:  realProcNamer{},
        selfPID:    uint32(os.Getpid()),
    }
}

func (s *WindowsProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
    if !srcIP.Is4() {
        return "", ""
    }
    key := cacheKey(network, srcIP, srcPort)
    if name, pkg, ok := s.cache.get(key); ok {
        return name, pkg
    }

    start := time.Now()
    name := s.lookupUncached(network, srcIP, srcPort)
    if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
        slog.Info("DIAG: process-lookup-slow",
            "platform", "windows",
            "network", network,
            "latencyMs", elapsed.Milliseconds())
    }

    s.cache.set(key, name, "")
    return name, ""
}

func (s *WindowsProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) string {
    srcBytes := srcIP.As4()
    switch network {
    case "tcp":
        return s.findInTCP(srcBytes, srcPort)
    case "udp":
        return s.findInUDP(srcBytes, srcPort)
    }
    return ""
}

func (s *WindowsProcessSearcher) findInTCP(srcAddr [4]byte, srcPort uint16) string {
    rows, err := s.tcpFetcher.Fetch()
    if err != nil {
        return ""
    }
    for i := range rows {
        r := &rows[i]
        if r.OwningPID == 0 || r.OwningPID == s.selfPID {
            continue
        }
        if ntohs(r.LocalPort) != srcPort {
            continue
        }
        if uint32ToIPv4(r.LocalAddr) != srcAddr {
            continue
        }
        return s.procNamer.NameForPID(r.OwningPID)
    }
    return ""
}

func (s *WindowsProcessSearcher) findInUDP(srcAddr [4]byte, srcPort uint16) string {
    rows, err := s.udpFetcher.Fetch()
    if err != nil {
        return ""
    }
    for i := range rows {
        r := &rows[i]
        if r.OwningPID == 0 || r.OwningPID == s.selfPID {
            continue
        }
        if ntohs(r.LocalPort) != srcPort {
            continue
        }
        if uint32ToIPv4(r.LocalAddr) != srcAddr {
            continue
        }
        return s.procNamer.NameForPID(r.OwningPID)
    }
    return ""
}
```

- [ ] **Step 7.4: Cross-compile + vet**

```bash
GOOS=windows go vet ./provider/
GOOS=windows go build ./provider/
```

Expected: both PASS.

- [ ] **Step 7.5: Commit**

```bash
git -C k2 add provider/process_windows.go provider/process_windows_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "feat(provider): WindowsProcessSearcher with mock-fetcher unit tests" \
  -m "Main searcher logic + 9 unit tests covering: IPv6 early return, TCP/UDP
match, PID=0 (TIME_WAIT) skip, self-PID skip, cache hit, negative cache,
empty table. Tests use interface-seam mocks — runnable on any Windows machine
via go test ./provider/. Real syscall fetcher exercised only by integration
smoke tests in Phase 7."
```

---

## Phase 4 — Daemon Wiring

### Task 8: Daemon factory + build-tag closure

**Files:**
- Create: `k2/daemon/process_linux.go`
- Create: `k2/daemon/process_windows.go`
- Modify: `k2/daemon/process_other.go`

- [ ] **Step 8.1: Read current `process_other.go` for reference**

```bash
cat k2/daemon/process_other.go
```

Confirm current build tag is `!darwin || ios`.

- [ ] **Step 8.2: Create `daemon/process_linux.go`**

Create `k2/daemon/process_linux.go`:

```go
//go:build linux

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns a Linux desktop ProcessSearcher.
// Desktop builds pass nil PackageResolver — Android-only UID→package mapping
// is wired separately in appext.
func newProcessSearcher() provider.ProcessSearcher {
    return provider.NewLinuxProcessSearcher(nil)
}
```

- [ ] **Step 8.3: Create `daemon/process_windows.go`**

Create `k2/daemon/process_windows.go`:

```go
//go:build windows

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns a Windows ProcessSearcher backed by
// GetExtendedTcpTable / GetExtendedUdpTable.
func newProcessSearcher() provider.ProcessSearcher {
    return provider.NewWindowsProcessSearcher()
}
```

- [ ] **Step 8.4: Modify `daemon/process_other.go` build tag**

Replace `k2/daemon/process_other.go`:

```go
//go:build !darwin && !linux && !windows

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns nil on platforms that don't support
// process-name attribution from the daemon shell (e.g. iOS, where appext
// owns the engine and wires its own searcher).
func newProcessSearcher() provider.ProcessSearcher {
    return nil
}
```

- [ ] **Step 8.5: Verify all three platforms compile**

```bash
GOOS=darwin  go build ./...
GOOS=linux   go build ./...
GOOS=windows go build ./...
```

Expected: all 3 PASS.

- [ ] **Step 8.6: Run daemon tests on host platform**

```bash
go test ./daemon/ -v
```

Expected: PASS (existing daemon tests).

- [ ] **Step 8.7: Commit**

```bash
git -C k2 add daemon/process_linux.go daemon/process_windows.go daemon/process_other.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "feat(daemon): wire ProcessSearcher factories for Linux + Windows desktop" \
  -m "daemon/process_linux.go: NewLinuxProcessSearcher(nil) (desktop, no
PackageResolver). daemon/process_windows.go: NewWindowsProcessSearcher().
process_other.go build tag tightened from '!darwin || ios' to
'!darwin && !linux && !windows' for explicit matrix. iOS unaffected
(appext owns its own searcher path)."
```

---

## Phase 5 — Documentation

### Task 9: k2/CLAUDE.md — DIAG event + Lock Ordering Graph

**Files:**
- Modify: `k2/CLAUDE.md`

- [ ] **Step 9.1: Read current DIAG table + Lock Ordering Graph for insertion points**

```bash
grep -n "DIAG: pipe-watchdog\|connTracker.mu" k2/CLAUDE.md
```

Note the line numbers — append after these existing entries.

- [ ] **Step 9.2: Add `DIAG: process-lookup-slow` to reserved table**

Open `k2/CLAUDE.md` and locate the "Reserved event names" table. Find the row:

```
| `DIAG: pipe-watchdog` | WARN | pipe() watchdog force-closed a stuck half-closed pipe | firstExitDir, graceMs |
```

Add a row directly after it:

```
| `DIAG: process-lookup-slow` | INFO | latency > 100ms | platform, network, latencyMs |
```

- [ ] **Step 9.3: Add Lock Ordering Graph entries**

Locate the "Lock Ordering Graph" section. Find the standalone-locks block:

```
rule.Engine.mu (state, RWMutex, standalone — no nesting with engine locks)

config.Subscription.mu (state, standalone — no nesting with engine/daemon locks)
```

Add after them:

```
provider.processCache.mu (state, standalone — no nesting with engine/daemon locks)
provider.LinuxProcessSearcher.indexMu (state, RWMutex, standalone — no nesting with engine/daemon locks)
```

- [ ] **Step 9.4: Verify edits**

```bash
grep -n "process-lookup-slow\|processCache.mu\|LinuxProcessSearcher.indexMu" k2/CLAUDE.md
```

Expected: 3 matches.

- [ ] **Step 9.5: Commit**

```bash
git -C k2 add CLAUDE.md
git -C k2 diff --cached --name-only
git -C k2 commit -m "docs(k2): register process-lookup-slow DIAG + ProcessSearcher locks" \
  -m "Per k2 Diagnostic Logging Constitution and Lock Ordering Graph rules:
new DIAG event must appear in reserved names table; new mutexes must be
listed in the lock ordering graph. processCache.mu and indexMu are both
standalone (no engine/daemon lock nesting)."
```

---

## Phase 6 — Validation

### Task 10: Cross-compile validation via `make build-all-platforms`

**Files:** none modified — pure validation step.

- [ ] **Step 10.1: Run k2's cross-compile matrix**

```bash
cd k2 && make build-all-platforms
```

Expected output ends with: `All platform builds OK`

If failure: identify offending file, fix, repeat. Do NOT commit anything in this step — fix in the failing task's commit.

- [ ] **Step 10.2: Run race-enabled test suite**

```bash
cd k2 && go test -race -timeout 300s ./provider/ ./daemon/
```

Expected: all PASS, no race warnings.

---

### Task 11: Benchmark — hot cache <1μs verification

**Files:**
- Modify: `k2/provider/process_cache_test.go` (add benchmark — platform-neutral)

The benchmark targets the cache hot path (`cacheKey` + `processCache.get`), which is the dominant cost of `FindProcess` on a cache hit. Putting it in `process_cache_test.go` (no build tag) means it runs on macOS dev machines, not just Linux/Windows.

- [ ] **Step 11.1: Append benchmark to `process_cache_test.go`**

Append to `k2/provider/process_cache_test.go`:

```go
import (
    "net/netip"
    // existing imports retained
)

func BenchmarkProcessCache_HotPath(b *testing.B) {
    c := newProcessCache(processCacheCap, processCacheTTL)
    addr := netip.MustParseAddr("1.2.3.4")
    key := cacheKey("tcp", addr, 5678)
    c.set(key, "chrome", "")
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Mimic FindProcess hot path: key construction + map lookup.
        k := cacheKey("tcp", addr, 5678)
        c.get(k)
    }
    b.ReportAllocs()
}
```

- [ ] **Step 11.2: Run benchmark on host platform**

```bash
cd k2 && go test -bench BenchmarkProcessCache_HotPath -benchmem ./provider/ -run=^$
```

Expected: ns/op < 1000 (i.e. <1μs/op). Allocs/op should be 1 (string from cacheKey builder).

If higher than 1000ns: investigate. Likely culprit = lock contention under benchmark concurrency, or excessive allocation in cacheKey. Document actual measurement in commit message.

- [ ] **Step 11.3: Commit**

```bash
git -C k2 add provider/process_cache_test.go
git -C k2 diff --cached --name-only
git -C k2 commit -m "test(provider): benchmark hot-cache <1μs target" \
  -m "BenchmarkProcessCache_HotPath validates per-call latency budget for
the cache layer (the FindProcess hot path on cache hits). Build-tag-free
so it runs on any dev platform. Catches perf regression before shipping."
```

---

## Phase 7 — Submodule Pointer Bump

### Task 12: Bump k2 submodule pointer in parent worktree

**Files:**
- Modify: parent worktree submodule ref (via `git add k2`)

**Pre-condition:** k2 submodule starts in detached HEAD state (typical for submodules). The Phase 1-6 commits are accumulating on top of that detached HEAD. Before bumping the parent pointer, we name the new HEAD onto a feature branch so the commits aren't orphaned if anyone later checks out a different ref in the submodule.

- [ ] **Step 12.1: Confirm all k2 commits landed**

```bash
git -C k2 log --oneline -12
```

Expected: 9 new commits from this plan (Tasks 1, 2, 3, 4, 5, 6, 7, 8, 9, 11 — Task 10 is validation only, Task 12 is parent-side). The 10th expected line is the original detached-HEAD commit (`3257a21 perf(engine): skip ProcessSearcher when no rule references it`).

- [ ] **Step 12.2: Create / fast-forward k2 feature branch onto these commits**

```bash
git -C k2 checkout -B feat/win-linux-process-searcher
git -C k2 branch --show-current
```

Expected: `feat/win-linux-process-searcher` printed. The detached HEAD's commits are now reachable from this branch.

- [ ] **Step 12.3: Push k2 feature branch to origin**

```bash
git -C k2 push -u origin feat/win-linux-process-searcher
```

Expected: branch published. (If origin is a local mirror per `git -C k2 remote -v`, push is essentially copying into the local clone — still useful so the commits are tracked under a name.)

- [ ] **Step 12.4: Verify parent worktree sees the submodule change**

```bash
git status
```

Expected: `modified: k2 (new commits)` line in the parent worktree status.

- [ ] **Step 12.5: Commit submodule pointer bump in parent**

```bash
git add k2
git diff --cached
git commit -m "chore(k2): bump submodule — Win/Linux ProcessSearcher" \
  -m "Brings in 9 commits implementing detect path for Windows + Linux
desktop daemon. Closes App Bypass v0.4.5 GA gate (≥3 platforms; reaches
4: macOS + Android + Linux desktop + Windows). k2 branch: feat/win-linux-process-searcher."
```

- [ ] **Step 12.6: Final smoke summary**

Run a final sanity check from the worktree root:

```bash
git log --oneline -3
git -C k2 log --oneline -10
git -C k2 branch --show-current
```

Verify the parent's last commit is the submodule bump, the k2 branch is `feat/win-linux-process-searcher`, and 9 new commits sit on top of the prior k2 HEAD.

---

## Manual Smoke Checklist (post-implementation, pre-merge)

This is NOT a task — it's a checklist for the user to run before merging. Each platform needs a daemon dev build + a curl rule + a packet capture or DIAG log inspection.

```
[ ] macOS
    Build: GOOS=darwin go build -o /tmp/k2-darwin ./cmd/k2
    Connect with ClientConfig containing routes: [
      {via: direct, match: {process_name: ["curl"]}},
      {via: k2v5://..., match: {}}
    ]
    Run: curl https://ipinfo.io
    Verify: response comes from your real ISP IP (bypass match)
    Check daemon logs: grep "DIAG: connected" /var/log/kaitu/k2.log

[ ] Linux desktop
    Build: GOOS=linux go build -o /tmp/k2-linux ./cmd/k2
    Same ClientConfig template with process_name: ["curl"]
    Same curl test — verify bypass

[ ] Windows
    Build (from macOS cross): GOOS=windows go build -o /tmp/k2.exe ./cmd/k2
    Run on Windows dev machine
    ClientConfig with process_name: ["curl.exe"]  ← note .exe suffix
    Run: curl.exe https://ipinfo.io  (from CMD, not PowerShell alias)
    Verify bypass

[ ] DIAG slow event sanity
    Under all 3 platforms, rule should fire 1000+ times in normal browsing
    without `DIAG: process-lookup-slow` appearing more than ~3 times
    (cold rebuild + occasional /proc race). If slow event spam → investigate.
```

---

## Spec Coverage Self-Check

This plan implements every section of the spec:

| Spec § | Implemented in Task |
|--------|--------------------|
| §2.1 daemon/process_linux.go (new) | Task 8 |
| §2.1 daemon/process_windows.go (new) | Task 8 |
| §2.1 process_other.go build tag收窄 | Task 8 |
| §2.1 provider/process_linux.go 重构 | Tasks 3 + 4 |
| §2.1 provider/process_windows.go (new) | Tasks 5 + 6 + 7 |
| §2.1 provider/process_cache.go (new) | Task 1 |
| §2.1 provider/process_darwin.go 重构 | Task 2 |
| §2.1 Self-PID skip 三平台统一 | Tasks 2, 3, 7 (Darwin keeps existing lsof skip; Linux + Windows add defensive check) |
| §2.1 IPv4-only scope | Tasks 2, 3, 7 |
| §2.1 DIAG event | Tasks 3, 7 (emit) + Task 9 (register) |
| §2.1 Lock Ordering Graph 更新 | Task 9 |
| §2.1 单元测试 | Tasks 1, 2 (regression), 3, 4, 5, 7 |
| §2.1 Benchmark | Task 11 |
| §2.1 Cross-compile | Task 10 |
| §4 processCache helper details | Task 1 |
| §5.1-§5.5 Linux struct + FindProcess + lookupPID + buildInodeIndex | Tasks 3 + 4 |
| §6.1-§6.6 Windows API bindings + struct + helpers + searcher | Tasks 5 + 6 + 7 |
| §7 DIAG event | Tasks 3, 7, 9 |
| §8 Testing strategy | Tasks 1, 3, 4, 5, 7, 11 |
| §9 Performance budget | Task 11 (hot bench) + DIAG (cold tail) |
| §12 Release criteria | Tasks 10, 11 + Manual Smoke Checklist |
