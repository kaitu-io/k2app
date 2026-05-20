# Win/Linux 桌面 ProcessSearcher (App Bypass detect 路径)

**Date:** 2026-05-14
**Status:** Design — pending writing-plans handoff
**Scope:** Windows + Linux 桌面 daemon 的 `ProcessSearcher`（detect 连接→进程名），含 Darwin 缓存机制重构与共享 helper 抽取
**Out of scope:** App enumeration (UI list)、Android (gomobile)、iOS、netlink-based Linux 实现

---

## 1. Background & Problem

App Bypass v0.4.5 ([`docs/superpowers/specs/2026-05-12-app-bypass-design.md`](2026-05-12-app-bypass-design.md)) 已经把"按 app 黑名单分流"做到 webapp + rule engine 层。

rule engine 在 `routeConn` 里调用 `ProcessSearcher.FindProcess(network, srcIP, srcPort)` 拿进程名，匹配 `process_name` 规则。当前矩阵：

| 平台 | ProcessSearcher | 状态 |
|------|----------------|------|
| macOS | `DarwinProcessSearcher` (lsof) | ✅ Phase 0 已修 2 个 upstream bug |
| Android | `LinuxProcessSearcher` + PackageResolver | ✅ 通过 appext 注入；SELinux 限制下 fallback 到 kernel-level `addDisallowedApplication` |
| Linux 桌面 | nil (`daemon/process_other.go`) | ❌ rule 永远不匹配 `process_name` |
| Windows | nil (`daemon/process_other.go`) | ❌ 同上 |
| iOS | nil | n/a (no daemon) |

**问题**：Win/Linux 桌面 App Bypass 配置后 rule 不触发，用户看到的是"按了开关但没用"。App Bypass spec §12.5 的发布门："v1 GA 至少 3 平台过"，目前只有 macOS + Android，欠一个桌面平台才能达标。

**目标**：把 detect 路径在 Win + Linux 桌面打通，复用并升级 macOS 的"很多优化"模式（cache、self-PID skip、IPv4 scope），针对各平台瓶颈分别优化。

App enumeration（UI 列表）三平台均已完整 —— macOS / Windows 走 Tauri `list_running_apps`，Linux 桌面走 daemon `/api/helper app-list-running`。本 spec 不涉及。

## 2. Scope

### 2.1 In Scope

| 项 | 说明 |
|---|---|
| `daemon/process_linux.go` (新) | 工厂返 `provider.NewLinuxProcessSearcher(nil)` |
| `daemon/process_windows.go` (新) | 工厂返 `provider.NewWindowsProcessSearcher()` |
| `daemon/process_other.go` build tag 收窄 | `!darwin && !linux && !windows` |
| `provider/process_linux.go` 重构 | 加 result cache + inode→PID 反向索引（5s TTL + stale-while-revalidate + singleflight） |
| `provider/process_windows.go` (新) | `WindowsProcessSearcher` via `GetExtendedTcpTable` / `GetExtendedUdpTable` + `OpenProcess` + `QueryFullProcessImageName` |
| `provider/process_cache.go` (新) | 跨平台共享 `processCache`：TTL + soft-cap lazy eviction |
| `provider/process_darwin.go` 重构 | 切换到共享 `processCache`（顺手补齐 Darwin 当前零淘汰的内存债） |
| Self-PID 防御性 skip | 三平台统一加（Darwin 是 bug 修复，Win/Linux 是防御性兜底） |
| IPv4-only scope | 三平台统一 `srcIP.Is4()` 早返回，与 `core/tunnel.go:493` 的 `Unmap()` 配合 |
| DIAG 事件 | 新增 `DIAG: process-lookup-slow`（INFO, threshold > 100ms），登记到 `k2/CLAUDE.md` 表 |
| Lock Ordering Graph 更新 | `processCache.mu`、`LinuxProcessSearcher.indexMu` 登记为 standalone |
| 单元测试 | `procRoot` 注入（Linux）+ interface seam fetcher mock（Windows）+ 共享 cache 测 |
| Benchmark | `BenchmarkFindProcess_HotCache` 验 < 1μs hot path |
| Cross-compile | `GOOS=windows go build ./...` 加入 `make pre-release` 矩阵（若未在） |

### 2.2 Out of Scope (v1)

- **App enumeration**（已完整，见 Background）
- **iOS**：appext 走另一条路径，无 daemon ProcessSearcher
- **Android 重构**：保留 `NewLinuxProcessSearcher(pr PackageResolver)` 签名不变，零修改；Android 同 commit 内获得缓存收益，但不重测 Android UAT
- **netlink NETLINK_SOCK_DIAG**：Linux v2 候选优化（单次 1ms 而非 50ms），本次只做 `/proc` 路径
- **IPv6 process attribution**：与 Darwin/Linux 现状一致，`!srcIP.Is4()` 早返空
- **PID → name 二级缓存（Windows）**：result cache 足以，不加
- **Per-table cache（Windows）**：每次 cache miss 单独 fetch 表，不加表级共享缓存
- **背景预热**：daemon 启动不预 build inode 索引（Android 路径不能等）
- **`process-lookup-empty-rate` 等比率类 DIAG**：复杂度高，靠 rule 匹配率间接观测

### 2.3 Out-of-Scope Side Effects

**Darwin `DarwinProcessSearcher` 内部缓存结构改变**：从私有 `sync.Mutex + map` 切到共享 `*processCache`。对外 `FindProcess` 签名不变。既有测试 `TestProcessSearcher_SkipsSelf`（commit `82025a9`）继续通过。

**`daemon/process_other.go` build tag 收窄**：原 `!darwin || ios` → 新 `!darwin && !linux && !windows`。iOS 不构建 daemon（appext only），实际无功能变化，但 build tag 语义更准确。

## 3. Architecture

### 3.1 Layer Mapping

```
                ┌──────────────────────────────────────┐
                │ core/tunnel.go::routeConn            │
                │   meta.ProcessName, meta.PackageName │
                │     = processSearcher.FindProcess()  │
                └────────────┬─────────────────────────┘
                             │
                ┌────────────┴─────────────┐
                │ provider.ProcessSearcher │
                │     (interface)          │
                └────────────┬─────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ▼                    ▼                    ▼
┌──────────────┐    ┌──────────────────┐  ┌───────────────────┐
│ Darwin       │    │ Linux            │  │ Windows           │
│  lsof exec   │    │  /proc/net/{tcp, │  │  GetExtendedTcp/  │
│  -F pc       │    │  udp}{,6}        │  │  UdpTable +       │
│  skip self   │    │  inode → PID via │  │  OpenProcess +    │
│              │    │  /proc/*/fd      │  │  QueryFullProc…   │
└──────┬───────┘    └──────┬───────────┘  └──────┬────────────┘
       │                   │                     │
       └───────────────────┼─────────────────────┘
                           │
                  ┌────────▼──────────┐
                  │ processCache      │  (TTL + soft cap)
                  │  (shared helper)  │
                  └───────────────────┘
```

### 3.2 File Layout

```
k2/daemon/
  process_darwin.go      [unchanged] returns &DarwinProcessSearcher{}
  process_linux.go       [NEW] returns provider.NewLinuxProcessSearcher(nil)
  process_windows.go     [NEW] returns provider.NewWindowsProcessSearcher()
  process_other.go       [build tag 收窄] !darwin && !linux && !windows

k2/provider/
  process.go             [unchanged] interface + NopProcessSearcher
  process_cache.go       [NEW] shared cache helper (build-tag free)
  process_darwin.go      [refactor] use shared *processCache
  process_linux.go       [refactor] use shared *processCache + inode index + procRoot
  process_linux_test.go  [extend] fake procfs + concurrent rebuild test
  process_windows.go     [NEW] WindowsProcessSearcher + fetcher seam
  process_windows_test.go [NEW] mock fetcher tests + ntohs / uint32→IPv4 unit tests
  process_test.go        [unchanged] Darwin self-skip test (still passes)
  process_stub.go        [unchanged] no-op for other platforms

k2/CLAUDE.md
  Reserved DIAG event names: 加 process-lookup-slow
  Lock Ordering Graph: 加 processCache.mu + LinuxProcessSearcher.indexMu
```

### 3.3 Daemon Wiring

```go
// daemon/process_linux.go
//go:build linux
package daemon
import "github.com/kaitu-io/k2/provider"
func newProcessSearcher() provider.ProcessSearcher {
    return provider.NewLinuxProcessSearcher(nil)
}

// daemon/process_windows.go
//go:build windows
package daemon
import "github.com/kaitu-io/k2/provider"
func newProcessSearcher() provider.ProcessSearcher {
    return provider.NewWindowsProcessSearcher()
}
```

`daemon.engineConfigFromClientConfig`（daemon.go:484）继续调用 `newProcessSearcher()`，build tag 自动路由。

## 4. Shared `processCache` Helper

```go
// provider/process_cache.go (build-tag free)

const (
    processCacheCap = 4096
    processCacheTTL = 2 * time.Second
)

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
        cap:     cap, ttl: ttl,
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
        for k, e := range c.entries {
            if now.After(e.expires) {
                delete(c.entries, k)
            }
        }
    }
    c.entries[key] = processCacheEntry{
        name: name, pkg: pkg,
        expires: now.Add(c.ttl),
    }
}
```

**关键性质**：
- TTL 2s（沿用 Darwin 当前值）
- Soft cap 4096 ≈ 200KB 最大内存
- Lazy eviction：`set()` 时若超 cap 才扫一次清过期
- 负缓存：空结果 `(name="", pkg="")` 也写入，防 retry 风暴
- `deadlock.Mutex`（项目约定，零开销 prod 构建）

**Lock Ordering Graph 登记**：

```
provider.processCache.mu (state, standalone — no nesting with engine/daemon locks)
```

## 5. Linux Desktop Detailed Design

### 5.1 Struct

```go
// provider/process_linux.go
//go:build linux

const inodeIndexTTL = 5 * time.Second

type LinuxProcessSearcher struct {
    packageResolver PackageResolver
    cache           *processCache

    indexMu      deadlock.RWMutex
    inodeIndex   map[uint64]int  // inode → PID snapshot
    indexExpires time.Time       // zero = never built

    sf       singleflight.Group  // dedup concurrent rebuilds
    procRoot string              // default "/proc", overridable for tests
}

func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher {
    return &LinuxProcessSearcher{
        packageResolver: pr,
        cache:           newProcessCache(processCacheCap, processCacheTTL),
        procRoot:        "/proc",
    }
}
```

**签名守恒**：`NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher` 不变 → Android `appext/process_linux.go` 零修改。

### 5.2 FindProcess

```go
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
            "platform", "linux", "network", network,
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
    pid := s.lookupPID(inode)
    if pid == 0 || pid == os.Getpid() {
        return "", ""  // 0 = no owner; selfPid = defensive skip
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

### 5.3 lookupPID — Stale-While-Revalidate

```go
func (s *LinuxProcessSearcher) lookupPID(inode uint64) int {
    s.indexMu.RLock()
    indexBuilt := !s.indexExpires.IsZero()
    pid := s.inodeIndex[inode]
    expired := indexBuilt && time.Now().After(s.indexExpires)
    s.indexMu.RUnlock()

    if !indexBuilt {
        // Cold start — sync rebuild (singleflight prevents thundering herd)
        s.sf.Do("rebuild", s.doRebuild)
        s.indexMu.RLock()
        pid = s.inodeIndex[inode]
        s.indexMu.RUnlock()
        return pid
    }

    if expired {
        // Stale — async rebuild, return stale value immediately
        safego.Go(func() { s.sf.Do("rebuild", s.doRebuild) })
    }
    return pid
}

func (s *LinuxProcessSearcher) doRebuild() (any, error) {
    idx := s.buildInodeIndex()
    s.indexMu.Lock()
    s.inodeIndex = idx
    s.indexExpires = time.Now().Add(inodeIndexTTL)
    s.indexMu.Unlock()
    return nil, nil
}
```

**设计依据**：
- Cold start 同步：daemon 启动后第一次 FindProcess 吃一次性 50-300ms 重建成本
- Stale 异步：5s 后第一个 caller 拿 stale 数据立即返回，后台 rebuild 完成后下一个 caller 拿新值
- Singleflight key `"rebuild"`：并发触发只跑一次

**Stale 数据风险**：PID 在 5s 内被回收并复用需 ~6500 process spawn/s（PID_MAX 32768），桌面不可能。如发生：`readProcessName(pid)` 返新进程名（错误归属）或 `""`（进程已退）。错归属概率 ≈ 0，可接受。

### 5.4 buildInodeIndex

```go
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

**性能预期**：~300 PID × ~30 socket fd = ~9000 readlink × ~1μs ≈ **10-50ms** 典型桌面；重负载（1000+ PID）~300ms。

**不并行化**：首次冷启动单次成本 OK；后续 stale-while-revalidate 异步，不阻塞 caller。

### 5.5 findSocketInode 重构（procRoot 注入）

原 `searchProcNet(path string, ...)` 已经接受 path 参数。改造点：
- 所有路径硬编码 `/proc/net/...` 替换为 `filepath.Join(s.procRoot, "net", ...)`
- 函数从包级提升到 method：`func (s *LinuxProcessSearcher) findSocketInode(...)`

`parseProcNetAddr` 保持纯函数（无 receiver），便于单测。

### 5.6 Lock Ordering Graph

```
provider.LinuxProcessSearcher.indexMu (state, RWMutex, standalone — no nesting with engine/daemon locks)
```

## 6. Windows Detailed Design

### 6.1 API Bindings

```go
// provider/process_windows.go
//go:build windows

var (
    iphlpapi                 = windows.NewLazySystemDLL("iphlpapi.dll")
    procGetExtendedTcpTable  = iphlpapi.NewProc("GetExtendedTcpTable")
    procGetExtendedUdpTable  = iphlpapi.NewProc("GetExtendedUdpTable")
)

const (
    AF_INET                 = 2
    TCP_TABLE_OWNER_PID_ALL = 5
    UDP_TABLE_OWNER_PID     = 1
)

type tcpRowOwnerPID struct {
    State        uint32
    LocalAddr    uint32  // network byte order
    LocalPort    uint32  // low 16 bits, network byte order
    RemoteAddr   uint32
    RemotePort   uint32
    OwningPID    uint32
}

type udpRowOwnerPID struct {
    LocalAddr uint32
    LocalPort uint32
    OwningPID uint32
}
```

### 6.2 Struct + Interface Seams

```go
type WindowsProcessSearcher struct {
    cache      *processCache
    tcpFetcher tcpTableFetcher
    udpFetcher udpTableFetcher
    procNamer  processNamer
    selfPID    uint32
}

type tcpTableFetcher interface {
    Fetch() ([]tcpRowOwnerPID, error)
}
type udpTableFetcher interface {
    Fetch() ([]udpRowOwnerPID, error)
}
type processNamer interface {
    NameForPID(pid uint32) string
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
```

**Interface seam 的价值**：mock fetcher 让所有匹配逻辑（端口/地址/PID 过滤）可单测，跑在 Windows runner 上。

### 6.3 Byte Order Helpers

```go
// dwLocalPort 字段：低 16 位存网络字节序的端口
// 例：物理端口 54321 (0xD431) → 网络字节序 [0xD4, 0x31]
//      存入 dwLocalPort 低 16 位 → uint32 值为 0x000031D4 (LE 内存)
// 提取：ntohs(0x31D4) = (0x31D4 << 8) | (0x31D4 >> 8) = 0xD431 = 54321
func ntohs(n uint32) uint16 {
    v := uint16(n & 0xFFFF)
    return (v >> 8) | (v << 8)
}

// dwLocalAddr 字段：4 字节存网络字节序 IP
// 例：IP 192.168.1.5 网络字节序 [0xC0, 0xA8, 0x01, 0x05]
//      在 LE uint32 = 0x0501A8C0
//      PutUint32(LittleEndian, 0x0501A8C0) 写出 [0xC0, 0xA8, 0x01, 0x05] ✓
func uint32ToIPv4(n uint32) [4]byte {
    var b [4]byte
    binary.LittleEndian.PutUint32(b[:], n)
    return b
}
```

### 6.4 FindProcess

```go
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
            "platform", "windows", "network", network,
            "latencyMs", elapsed.Milliseconds())
    }

    s.cache.set(key, name, "")  // Windows no package concept
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

// findInUDP 同构，去掉 RemoteAddr/RemotePort 字段
```

**不过滤 TCP state**：`OwningPID==0` 已经把 TIME_WAIT zombie row 过滤掉；SYN_SENT / SYN_RCVD / ESTAB 都视为有效连接。

### 6.5 realTCPFetcher (syscall)

```go
type realTCPFetcher struct{}

func (realTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
    var size uint32
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
    n := *(*uint32)(unsafe.Pointer(&buf[0]))
    if n == 0 {
        return nil, nil
    }
    raw := unsafe.Slice(
        (*tcpRowOwnerPID)(unsafe.Pointer(&buf[4])),
        int(n),
    )
    out := make([]tcpRowOwnerPID, n)
    copy(out, raw)  // copy before buf goes out of scope
    return out, nil
}
```

**unsafe.Slice 安全性**：`raw` 是基于 `buf` 的零拷贝视图。`copy(out, raw)` 把数据拷到 GC-managed 的 `out` slice，之后 `buf` 可被 GC 回收。返回 `out` 安全。

### 6.6 realProcNamer

```go
type realProcNamer struct{}

func (realProcNamer) NameForPID(pid uint32) string {
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

**用 `PROCESS_QUERY_LIMITED_INFORMATION` 而非 `PROCESS_QUERY_INFORMATION`** — daemon 以 service 身份运行，需要 query 系统保护进程（System、smss、csrss），LIMITED 权限够用且 ACL 更宽松。

## 7. DIAG Event

新增一个事件，登记到 `k2/CLAUDE.md` "Reserved event names"：

| Event | Level | Threshold | Fields |
|-------|-------|-----------|--------|
| `DIAG: process-lookup-slow` | INFO | latency > 100ms | platform, network, latencyMs |

**不加** `process-lookup-fail` 或 `process-lookup-empty-rate` —— 空结果在 socket race / SELinux 阻断等场景属正常，比率类指标实现复杂度高，靠 rule 匹配率间接观测。

## 8. Testing Strategy

### 8.1 Linux 单元测试（procRoot 注入）

```go
func setupFakeProcfs(t *testing.T) string {
    dir := t.TempDir()
    // /proc/net/tcp: socket inode 1001 owned by 192.168.1.5:54321 → PID 8000
    must(os.MkdirAll(filepath.Join(dir, "net"), 0755))
    must(os.WriteFile(filepath.Join(dir, "net/tcp"),
        []byte("  sl  local_address rem_address ... inode\n"+
               "   0: 0501A8C0:D431 00000000:0000 01 00000000:00000000 00:00000000 00000000     0        0 1001 ...\n"),
        0644))
    must(os.MkdirAll(filepath.Join(dir, "8000/fd"), 0755))
    must(os.Symlink("socket:[1001]", filepath.Join(dir, "8000/fd/3")))
    must(os.WriteFile(filepath.Join(dir, "8000/comm"), []byte("chrome\n"), 0644))
    return dir
}
```

| Test | 覆盖点 |
|------|--------|
| `TestFindProcess_IPv6Early` | `srcIP.Is4()==false` 立即返空，不访问 procfs |
| `TestFindProcess_CacheHit` | 第二次同 key 不重扫 procfs（用计数 mock 或检查文件 stat 时间） |
| `TestFindProcess_NegativeCache` | inode=0 也写入 cache |
| `TestLookupPID_ColdStart` | 首次调用 sync rebuild，返回正确 PID |
| `TestLookupPID_StaleAsyncRebuild` | 5s 后 caller 拿 stale，async rebuild 完成后下一次拿新 |
| `TestLookupPID_Singleflight` | 10 goroutine 并发 stale lookup，buildInodeIndex 只被调用 1 次（atomic counter 注入） |
| `TestSelfPIDSkip` | mock procfs 让 inode → self pid → 返空 |
| `TestParseProcNetAddrIPv4` / `TestParseProcNetAddrIPv6` | 已存在的纯函数测试保留 |

### 8.2 Linux 共享 cache 测试

```go
// process_cache_test.go (build-tag free)
func TestProcessCache_BasicGetSet(t *testing.T) {...}
func TestProcessCache_TTLExpiry(t *testing.T) {...}
func TestProcessCache_NegativeEntry(t *testing.T) {...}
func TestProcessCache_SoftCapEviction(t *testing.T) {
    c := newProcessCache(2, time.Second)
    c.set("k1", "a", "")
    time.Sleep(1100 * time.Millisecond)  // expire k1
    c.set("k2", "b", "")
    c.set("k3", "c", "")  // triggers sweep, k1 evicted
    // verify len == 2 with k2 + k3 fresh
}
```

### 8.3 Windows 单元测试（mock fetcher）

`//go:build windows`，Windows CI runner 跑。

| Test | 内容 |
|------|------|
| `TestNtohs_KnownValues` | `ntohs(0x000031D4)==54321`, `ntohs(0x00005000)==80`, `ntohs(0x0000BB01)==443` |
| `TestUint32ToIPv4_KnownValues` | `0x0501A8C0 → [192,168,1,5]`, `0x0100007F → [127,0,0,1]` |
| `TestFindProcess_IPv6Early` | IPv6 src 不调 fetcher |
| `TestFindProcess_TCPMatch` | mock fetcher 一行匹配 → 返 procNamer 的 name |
| `TestFindProcess_UDPMatch` | UDP 路径 |
| `TestFindProcess_PIDZeroSkipped` | OwningPID=0 → 返空 |
| `TestFindProcess_SelfPIDSkipped` | OwningPID=selfPID → 返空 |
| `TestFindProcess_CacheHit` | 第二次同 key 不调 fetcher（mock counter） |
| `TestFindProcess_NegativeCache` | 空结果命中 cache |
| `TestFindProcess_NoMatch` | 空 fetcher 返空 |

### 8.4 集成测试

```go
//go:build linux
func TestFindProcess_RealSelf(t *testing.T) {
    if os.Geteuid() != 0 && !canReadOwnProcFD() {
        t.Skip("requires root or readable /proc/self/fd")
    }
    l, _ := net.Listen("tcp", "127.0.0.1:0")
    defer l.Close()
    port := l.Addr().(*net.TCPAddr).Port
    s := NewLinuxProcessSearcher(nil)
    name, _ := s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), uint16(port))
    // self skip: 期望返空
    if name != "" {
        t.Errorf("expected self skip, got %q", name)
    }
}

//go:build windows
func TestFindProcess_RealSelf(t *testing.T) { /* 同构 */ }
```

### 8.5 Benchmark

```go
func BenchmarkFindProcess_HotCache(b *testing.B) {
    s := NewLinuxProcessSearcher(nil)
    s.cache.set("tcp:1.2.3.4:5678", "chrome", "")
    addr := netip.MustParseAddr("1.2.3.4")
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        s.FindProcess("tcp", addr, 5678)
    }
}
// 目标: < 1μs/op
```

### 8.6 Cross-compile 验证

`make pre-release` 已经走 `build-all-platforms`（Makefile:171），矩阵已含 `build-windows-amd64` / `build-linux-amd64` / `build-linux-arm64` / `build-darwin-arm64`。**本 spec 不新增 Makefile target**，依赖既有矩阵 catch 编译错误。

实施时手动验证一次：

```bash
make build-all-platforms  # 通过 = 三平台 build tag 矩阵 OK
GOOS=windows GOARCH=arm64 go build ./...  # ARM64 Windows 兜底（Surface Pro）
```

Windows 单测在 Windows CI runner 上跑（k2 仓库需新增 `.github/workflows/` Windows test job，或承认 Windows 测试仅手动 — 本 spec 选**承认手动**，写入 §12.2 release gate）。

## 9. Performance Budget

| 平台 | Cold (cache miss) | Hot (cache hit) | 验证手段 |
|------|------------------|-----------------|---------|
| Darwin | <50ms (lsof exec) | <1μs | 既有 + DIAG slow |
| **Linux** | **<300ms 冷启动一次性 / <10ms 后续 stale 异步** | **<1μs** | bench hot + DIAG slow + stale-while-revalidate 测 |
| **Windows** | **<10ms** | **<1μs** | bench hot + DIAG slow |

## 10. Risk Matrix

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| Android `NewLinuxProcessSearcher` 签名退化 | 极低 | 编译失败 | 签名守恒，编译期保证 |
| Darwin 既有 self-skip 测试退化 | 极低 | CI 失败 | 测试保留，缓存重构对外行为不变 |
| Windows 字节序错误 | 中（易踩） | 100% 静默匹配失败 | 具体值单测 `TestNtohs_KnownValues` + 推演公开在 §6.3 |
| Linux stale 期间 PID 复用 | 极低（量化 ~0） | 错误归属 | 接受，DIAG 监控 |
| Linux 桌面用户 daemon 非 root | 低 | `/proc/*/fd` permission denied | systemd / sysvinit 安装即 root，CLI 用户不直接装 daemon |
| Windows `iphlpapi.dll` 缺失 | ~0 | fetcher err | LazyDLL 失败 → 返 err → 退化到 nil 行为（rule 无 process_name 匹配），daemon 启动 WARN log |
| `unsafe.Slice` lifetime | 低 | crash | `copy(out, raw)` 后立即返回 |
| TIME_WAIT zombie row | 中（常见） | 错归属为 PID 0 | `OwningPID==0` 过滤 |
| Dual-stack IPv6 socket on v4 lookup | 低 | miss → 负缓存 → DIAG 可见 | v1 接受，v2 加 v4-mapped 反扫 |
| 长时间运行 cache 内存膨胀 | 低 | 内存浪费 | Soft cap 4096，lazy eviction，最大 ~200KB |
| inode 索引重建并发抖动 | 低 | 重复 CPU | singleflight 保护 |
| iOS daemon 误触发 build | ~0 | 编译失败 | iOS 不构建 daemon (appext only)，build tag 矩阵兜底 |

## 11. Confidence Matrix

| 维度 | 信心 | 验证手段 |
|------|------|---------|
| 架构 — 文件矩阵 + build tag | 10/10 | 显式 |
| 守恒 — Android `NewLinuxProcessSearcher(pr)` 签名不变 | 10/10 | 编译期 |
| 守恒 — Darwin `FindProcess` 行为不变 | 10/10 | 既有 self-skip 测试通过 |
| Linux — stale-while-revalidate | 10/10 | cold/stale/fresh 三态单测 |
| Linux — singleflight 防 herd | 10/10 | atomic counter 并发单测 |
| Linux — procRoot 注入可测 | 10/10 | fake procfs fixture |
| Windows — API 签名 (`GetExtendedTcpTable`) | 10/10 | MSDN + Go syscall 标准模式 |
| Windows — 字节序处理 | 10/10 | 具体值单测 + §6.3 推演 |
| Windows — `unsafe.Slice` 安全 | 10/10 | `copy` 出来即安全 |
| Windows — mock fetcher 测试 | 10/10 | interface seam |
| Self-PID 防御 | 10/10 | 三平台单测 |
| Cache — TTL + soft cap | 10/10 | 共享 helper 单测 |
| Cache — 内存 bounded | 10/10 | cap × entry size 计算 |
| DIAG — `process-lookup-slow` 登记 | 10/10 | k2/CLAUDE.md 表新增 |
| Lock Ordering — standalone | 10/10 | 不嵌套 engine/daemon locks |
| 性能 — hot path < 1μs | 10/10 | bench |
| 性能 — cold path < 10ms (Win) / <300ms (Linux) | 10/10 | DIAG slow 兜底 |
| Cross-compile | 10/10 | `GOOS=windows go build` |

## 12. Release Criteria

### 12.1 Build Gates

- `make quick-check` 通过（含 `go vet`, `golangci-lint`, `go test -short -race`）
- `GOOS=windows go build ./...` 通过
- `GOOS=windows go vet ./...` 通过

### 12.2 Test Gates

- Linux 单测全过（fake procfs + 共享 cache + Linux RealSelf 集成测）— 在 macOS 上 `GOOS=linux go test` 可跑（无 syscall 依赖），实际 RealSelf 走 Linux dev 机或 CI
- Windows 单测全过（mock fetcher + ntohs + uint32→IPv4）— 在 Windows dev 机手动跑；Windows CI runner 加为 v2 ticket
- Darwin 既有测试全过（self-skip 不退化）
- Benchmark `BenchmarkFindProcess_HotCache` <1μs/op（任一平台）

### 12.3 Smoke Gates（手动）

- macOS daemon 启动 → 连接 → 配 `process_name: ["curl"]` rule → `curl https://...` 走 bypass，验 DIAG `connected` log
- Linux daemon 启动 → 同上（rule `process_name: ["curl"]`，命令 `curl https://...`）
- Windows daemon 启动 → rule `process_name: ["curl.exe"]`，命令 `curl.exe https://...`（CMD/PowerShell 内置 `curl.exe` 即 Windows 自带的真 curl，不是 PowerShell alias）。注意 Windows `filepath.Base` 保留 `.exe` 后缀，rule 必须带后缀才匹配
- 三平台 DIAG `process-lookup-slow` 在压测下不应频繁触发（rule 命中 1000 次内 < 3 次 slow event）

### 12.4 App Bypass v0.4.5 GA 门

完成本 spec 后，App Bypass spec §12.5 的"v1 GA 至少 3 平台过"门：
- macOS ✅
- Android ✅
- **Linux 桌面 ✅（本 spec 完成后）**
- **Windows ✅（本 spec 完成后）**

4 平台通过，超出 spec 要求。

## 13. Future Work (v2 ticket candidates)

- **Linux netlink NETLINK_SOCK_DIAG**：单次 1ms vs 当前 stale-while-revalidate 平均 ~ms，但实现复杂度（RTM_GETSOCK NLM_F_DUMP 二进制解析）远高。如 production DIAG 显示 stale 异常频繁可上
- **IPv6 process attribution**：扫 `/proc/net/tcp6` 加 v4-mapped 反扫；Windows 加 `MIB_TCP6ROW_OWNER_PID`
- **PID → name 二级缓存（Windows）**：如 DIAG 显示 `OpenProcess+QueryFullProcessImageName` 是热点
- **Per-table cache（Windows）**：100ms TTL 全表缓存，跨多 src port 共享
- **Process attribution for k2r gateway**：当前 k2r 不调 ProcessSearcher（router 场景下 source 是 LAN 设备，不是本机进程）
- **App enumeration unification**：当前 macOS/Win 走 Tauri、Linux 走 daemon — 形态不一致；不影响功能但工程上可考虑统一

## 14. References

- App Bypass spec: [`2026-05-12-app-bypass-design.md`](2026-05-12-app-bypass-design.md)
- k2 macOS attribution bug 修复 memory: project memory `k2-macos-attribution-bugs-fixed`
- k2 Diagnostic Logging Constitution: `k2/CLAUDE.md` § "Diagnostic Logging Constitution"
- k2 Concurrency Rules: `k2/CLAUDE.md` § "Concurrency Rules"
- k2 Lock Ordering Graph: `k2/CLAUDE.md` § "Lock Ordering Graph"
- Provider layer: `k2/provider/CLAUDE.md`
- Daemon layer: `k2/daemon/CLAUDE.md`
- Darwin reference impl: `k2/provider/process_darwin.go`
- Linux reference impl (Android): `k2/provider/process_linux.go`
- Tauri app enumeration: `desktop/src-tauri/src/app_list.rs`
- Linux daemon app enumeration: `k2/daemon/helper_app_list_linux.go`
- MSDN `GetExtendedTcpTable`: https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable
- MSDN `QueryFullProcessImageNameW`: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-queryfullprocessimagenamew
