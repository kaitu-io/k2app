# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux fallback).
Two binaries: `k2` (client) and `k2s` (server). Client has daemon architecture with HTTP localhost API.

## Quick Commands

```bash
go build -o k2 ./cmd/k2            # Build client
go build -o k2s ./cmd/k2s          # Build server
GOOS=linux go build -o k2r ./cmd/k2r  # Build gateway (Linux only)
go test ./...                      # All tests (wire + core + dns + appext + daemon + server + e2e)
go test -short ./...               # Fast: skip live DNS/network tests
go test -race -timeout 300s ./wire/...  # Wire tests with race (needs 300s+ timeout)
make quick-check                   # go vet + golangci-lint + go test -short -race
make gateway-check                 # gateway/ vet + build + unit tests (native Linux) or cross-compile (macOS)
make gateway-uat                   # Docker UAT: 10 phases, ~56 tests, full soft-router validation
make pre-release                   # quick-check + gateway-check + full tests + contract tests + cross-compile
make appext-ios                    # iOS xcframework (gomobile bind)
make appext-android                # Android aar (gomobile bind)
k2 setup <k2v5://URL>              # First-time: generate config + install service + start
k2 setup <k2subs://URL>            # First-time: subscription URL (auto-selects server)
k2 up                              # Connect (via daemon, uses saved config)
k2 down                            # Disconnect
k2 status                          # Connection status
k2 run                             # Start daemon (service entry point)
k2s run                            # Deploy server (auto-config, prints URL)
k2s setup                          # Server: interactive setup wizard
k2s setup --port 443 --username alice --no-firewall  # Automated setup
k2s user add <name>                # Add user, print connect URL
docker compose up --build           # Docker: server + init container (docker/)
make release VERSION=1.0.0         # Build all platforms + upload to S3
```

## Code Style

- **Minimalist**: Avoid over-engineering, prefer concrete types over interfaces
- **Stdlib first**: `os.Args` + `flag.NewFlagSet` for CLI, `net/http` for HTTP
- **Error wrapping**: Always `fmt.Errorf("context: %w", err)`
- **Trust protocol semantics**: When a protocol (QUIC, TLS, smux) provides built-in state management (keepalive, idle timeout, connection liveness), use its native mechanisms — don't layer application-level health checks or manual state tracking on top. Duplicating protocol-layer responsibilities in application code causes misclassification bugs (e.g. conflating "server can't reach destination" with "transport is broken") and unnecessary complexity.
- **Deadlock detection**: All mutexes use `deadlock.Mutex`/`deadlock.RWMutex` from `github.com/sasha-s/go-deadlock`. Production builds use `-tags=deadlock_disable` for zero overhead. Tests run with detection enabled (5s timeout). New mutex fields must use `deadlock.Mutex`, never `sync.Mutex`. `sync.WaitGroup`, `sync.Once`, `sync.Pool` stay as stdlib.

## Diagnostic Logging Constitution (never violate)

All diagnostic logs use `log/slog` (stdlib). The `DIAG:` prefix is reserved for operational diagnostic logs that enable field troubleshooting without DEBUG mode.

### Three Layers

| Layer | Level | Trigger | Purpose | grep |
|-------|-------|---------|---------|------|
| 1. Heartbeat | INFO | Every 30s (healthMonitor) | "Is the tunnel alive and healthy?" | `grep "DIAG: heartbeat"` |
| 2. Events | INFO/WARN | On anomaly (threshold-based) | "What went wrong?" | `grep "DIAG:" \| grep -v heartbeat` |
| 3. Full trace | DEBUG | All operations (existing logs) | "Exactly what happened?" | Switch level at runtime |

### Layer 1 Rules — Heartbeat

- Emitted every 30 seconds from `healthMonitor.sample()`.
- Fixed prefix: `"DIAG: heartbeat"`.
- Required fields: `health`, `transport`, `loss` (smoothed), `rttMs`, `minRttMs`, `txMB`, `rxMB`, `tcpConns`, `udpConns`, `uptimeS`, `fallback`, `heapMB`, `sysMB`, `goroutines`.
- Human-readable sizes (MB) and durations (ms/s).
- MUST NOT log at 1Hz — only every 30 samples.

### Layer 2 Rules — Events

Every Layer 2 log MUST:
1. Use `"DIAG: <event-name>"` as the message (e.g. `"DIAG: dns-slow"`, `"DIAG: proxy-dial-fail"`).
2. Include context fields that answer "where" and "why" (domain, addr, transport, error, latency).
3. Use INFO for performance observations, WARN for failures.
4. Be threshold-gated — do NOT log every slow query, only above threshold.

Reserved event names:

| Event | Level | Threshold | Fields |
|-------|-------|-----------|--------|
| `DIAG: dns-slow` | INFO | latency > 500ms | domain, upstream, latencyMs |
| `DIAG: dns-fail` | WARN | on error | domain, upstream, err |
| `DIAG: proxy-dial-fail` | WARN | on error | dest, transport, err |
| `DIAG: proxy-dial-slow` | INFO | latency > 3s | dest, transport, latencyMs |
| `DIAG: quic-handshake-fail` | WARN | on handshake error | addr, consecutiveFails, err |
| `DIAG: transport-switch` | WARN | on QUIC↔TCP-WS transition | from, to, reason |
| `DIAG: udp-relay-timeout` | WARN | consecutive > 2 | consecutiveCount, threshold |
| `DIAG: conn-highwater` | INFO | active > 100 | tcpCount, udpCount |
| `DIAG: wire-handshake` | INFO | handshake probe ok | handshakeMs, transport |
| `DIAG: wire-handshake-fail` | WARN | handshake probe failed | handshakeMs, err |
| `DIAG: connected` | INFO | engine connected | server, mode, dialMs |
| `DIAG: session-end` | INFO | engine stop | uptimeS, txMB, rxMB |
| `DIAG: wire-error` | WARN | on classified error | code, category, message |
| `DIAG: wake` | WARN | wall clock jump > 5s | sleepS, health, transport, uptimeS |
| `DIAG: dns-proxy-timeout` | WARN | proxy DNS 5s timeout | server, domain, connAgeS, isNewConn |
| `DIAG: dns-proxy-write-fail` | WARN | WriteTo failed | server, domain, connAgeS, isNewConn, err |
| `DIAG: dns-proxy-conn-dead` | WARN | recv loop died | server, domain, connAgeS, isNewConn, err |
| `DIAG: dns-proxy-dial-fail` | WARN | DialUDP failed | server, domain, err |
| `DIAG: transport-race-start` | INFO | race begins | candidates |
| `DIAG: transport-race-winner` | INFO | race complete | winner, winnerMs, tried |
| `DIAG: transport-race-fail` | WARN | all candidates failed | quic443Err, quicHopErr, tcpwsErr |
| `DIAG: echo-probe-ok` | DEBUG | echo succeeded | transport, rttMs |
| `DIAG: echo-probe-fail` | WARN | echo failed | transport, err |
| `DIAG: echo-probe-unsupported` | WARN | server doesn't support echo (old k2v5) | transport |
| `DIAG: transport-rerace` | WARN | runtime re-race (3 consecutive echo fails) | reason, previousTransport |
| `DIAG: dns-proxy-recv-no-callback` | WARN | DNS response arrived after callback expired | server, queryID, domain, numCallbacks |
| `DIAG: datagram-readloop-exit` | WARN | QUIC datagram read loop terminated | totalReceived, err |
| `DIAG: subs-refresh-fail` | WARN | subscription background refresh failed | endpoint, err |
| `DIAG: rule-bundle-stale` | WARN | engine.Start fell through to stale .k2b after fresh download failed | err, bundleCount, cacheDir |
| `DIAG: rule-bundle-empty` | WARN | downloaded tarball extracted 0 .krs (stale mirror / format skew) — rejected to avoid poisoning the cache | cacheDir, reason |
| `DIAG: rule-manifest-unreachable` | WARN | every manifest mirror failed — ensureOnce falls back to the legacy blind download (24h backstop honored first) | err, dir |
| `DIAG: rule-pin-fallback` | WARN | the manifest's winning mirror failed the tarball fetch — falling back to racing the remaining mirrors | pinned, fallbacks |
| `DIAG: rule-bundle-missing-region` | WARN | a configured match.region route has no usable bundle — engine.Start fails loud (504) instead of silently degrading to all-proxy | regions, cacheDir |
| `DIAG: quic-dial-orphan-cleanup` | WARN | orphaned transport.Dial goroutine resolved | addr, blockedMs |
| `DIAG: pipe-watchdog` | WARN | pipe() watchdog force-closed a stuck half-closed pipe | firstExitDir, graceMs |
| `DIAG: process-lookup-slow` | INFO | latency > 100ms | platform, network, latencyMs |
| `DIAG: udp-deliver-drop` | DEBUG | server session readCh full — handler stuck/slow | transport, sessionID, addr, len |
| `DIAG: stream-killed-on-write-fail` | WARN | per-Write deadline (5s) exceeded or write error on UDP relay mux | transport, side, sessionID, len, writeMs, err |
| `DIAG: rule-tmp-pin` | INFO | new IP pinned to tmp rule, or target changed for already-pinned IP (duplicates suppressed) | ip, target, action ("add"/"change"), prevTarget (change only), tmpSize |
| `DIAG: rule-tmp-reload` | INFO | rule engine reload cleared the DNS-learned tmp cache | cleared, routes, bundles |
| `DIAG: control-plane-relay` | INFO | on node relay response | origin, method, path, status, bytes |
| `DIAG: control-plane-relay-fail` | WARN | on node relay error | origin, path, err |
| `DIAG: relay-fetch` | INFO | on relay response | centerHost, method, path, status, nodeIP |
| `DIAG: relay-fetch-fail` | WARN | on relay dial/roundtrip error | centerHost, path, nodeIP, err |

### Layer 3 Rules — Full Trace

- Switch at runtime: `curl -X POST http://127.0.0.1:1778/api/log-level -d '{"level":"debug"}'`
- No changes to existing DEBUG logs. They are already comprehensive.
- When adding new code, use DEBUG for per-operation details, DIAG for threshold-gated observations.

### grep Playbook

```bash
# Quick health check (is it working?)
grep "DIAG: heartbeat" k2.log | tail -20

# Find problems (what went wrong?)
grep "DIAG:" k2.log | grep -v heartbeat

# DNS layer issues
grep "DIAG: dns" k2.log

# Transport layer issues
grep "DIAG: quic\|DIAG: transport" k2.log

# Sleep/wake events
grep "DIAG: wake" k2.log

# Proxy DNS failure breakdown (which layer failed?)
grep "DIAG: dns-proxy" k2.log

# Session summary
grep "DIAG: connected\|DIAG: session-end" k2.log

# All WARN-level diagnostics
grep "level=WARN" k2.log | grep "DIAG:"
```

### Adding New DIAG Events

1. Pick a descriptive event name (kebab-case, 2-3 words).
2. Add to the reserved event names table above.
3. Gate with a threshold — never log on every occurrence.
4. Include "where" + "why" fields.
5. Use INFO for observations, WARN for failures.

## Concurrency Rules (Constitutional — never violate)

### Core Principle

**A critical section is a pure function of in-memory state.**

Between `Lock()` and `Unlock()`, the only operations allowed are reads and writes to fields on the struct the mutex protects. Nothing else. No function calls that leave the struct boundary, no I/O, no waiting. If you can't describe the critical section as "read/write these fields," it's wrong.

Go mutexes are non-reentrant, have no ownership tracking, and provide no priority inheritance. `go-deadlock` detects locks held >30s at runtime, but runtime detection is a safety net — **not a substitute for correct design**. A deadlock path that exists will eventually be reached given enough runtime.

### Two Lock Categories

**State lock** (`mu`) — protects struct fields. Critical section: read/write fields only. Duration: microseconds.

**Serialization lock** (`writeMu`, `connectMu`) — serializes access to a non-thread-safe resource (stream writer, expensive connect path). May hold across I/O intentionally. Must NEVER be nested inside a state lock.

All rules below apply to **state locks**. Serialization locks have their own constraints: they must not be acquired while holding any state lock, and their ordering must be documented.

### Lock Ordering Graph

**Always acquire in this order; never reverse.** Every new mutex must be added here.

```
engine.mu (state)
  → healthMonitor.mu (state)
  → daemon.mu (state) → daemon.sseMu (state)
  → TransportManager.mu (state) → QUICClient.mu (state)
  →                                TCPWSClient.mu (state)
  → dnsOverride.mu (state)
  → connTracker.mu (state)

Serialization locks (never nested inside any state lock above):
  QUICClient.connectMu → QUICClient.mu
  QUICClient.initUDPMu → QUICClient.mu
  TCPWSClient.initUDPMu → TCPWSClient.mu
  (TCPWSClient dedupes concurrent dials via singleflight.Group — no held mutex)
  smuxUDPMux.writeMu (standalone)
  quicStreamUDPMux.writeMu (standalone)
  udpOverflowStream.initMu → udpOverflowStream.writeMu

rule.Engine.mu (state, RWMutex, standalone — no nesting with engine locks)

config.Subscription.mu (state, standalone — no nesting with engine/daemon locks)

wire.RelayManager.mu (state, standalone — control-plane relay node pool/ranking/health; network I/O (client.Do) and Transport.CloseIdleConnections happen OUTSIDE the lock)

provider.processCache.mu (state, standalone — no nesting with engine/daemon locks)
provider.LinuxProcessSearcher.indexMu (state, RWMutex, standalone — no nesting with engine/daemon locks)

gateway standalone locks (no nesting with engine locks):
  Gateway.opMu (serialization) → Gateway.mu (state) → Gateway.sseMu (state)
  RouterDeviceManager.mu (state, RWMutex — acquired after Gateway.mu released)
  nftNetlinkInterceptor.mu (serialization — acquired after all state locks released)

k2p/signal standalone locks (no nesting with engine locks):
  signal.Store.mu (state, RWMutex) → PeerEntry.WriteMu (serialization)
  signal.SessionRouter.mu (state)
  wsConn.writeMu (serialization, standalone — protects ws.WriteJSON)
  CandidateExchange.writeMu (serialization, standalone — protects ws.WriteJSON)
  k2pOutbound.connectMu (serialization) → k2pOutbound.mu (state)
```

### The Five Forbidden Patterns

**1. No external calls under lock** — NEVER call interface methods, methods on other structs, callbacks, or closures while holding a state lock. This includes: `handler.OnStatus()`, `resource.Close()`, any func parameter or captured closure. The callee's implementation is outside your control. Pure stdlib value functions (`time.Now`, `fmt.Sprintf`, `errors.New`, `math.Max`) and same-struct field-only helpers (no I/O, no additional locks) are allowed.
```go
// WRONG: handler.OnStatus() is an external call
e.mu.Lock()
e.state = StateConnected
e.handler.OnStatus(e.buildStatusLocked())  // external call under lock
e.mu.Unlock()

// RIGHT: build snapshot under lock, notify outside
e.mu.Lock()
e.state = StateConnected
handler := e.handler
status := e.buildStatusLocked()
e.mu.Unlock()
if handler != nil {
    handler.OnStatus(status)  // no lock held
}
```

**2. No I/O under lock** — NEVER hold a state lock across any operation with unbounded duration: network I/O, DNS resolution, TLS/QUIC handshake, `OpenStreamSync`, CGo/FFI calls (SCDynamicStore, CFRunLoop), `exec.Command`, file I/O on network mounts, `resource.Close()` (may flush, do protocol teardown, or CGo cleanup).
```go
// WRONG: QUIC dial blocks 5-30s under lock
mu.Lock()
conn, err := transport.Dial(ctx, addr)
mu.Unlock()

// RIGHT: check-release-act-reacquire
mu.Lock()
if closed { mu.Unlock(); return }
mu.Unlock()
conn, err := transport.Dial(ctx, addr)  // no lock held
mu.Lock()
if closed { /* cleanup conn */ mu.Unlock(); return }
// store result
mu.Unlock()
```

**3. No waiting under lock** — NEVER call `wg.Wait()`, `<-chan`, `sync.Cond.Wait()`, or any blocking receive while holding a state lock. If the goroutine being waited on needs the same lock, deadlock is guaranteed.
```go
// WRONG: health goroutine needs e.mu to exit
e.mu.Lock()
e.health.stop()  // calls wg.Wait()
e.mu.Unlock()

// RIGHT: extract, nil, release, then wait
e.mu.Lock()
hm := e.health
e.health = nil
e.mu.Unlock()
hm.stop()  // wg.Wait() with no lock held
```

**4. No Close() under state lock** — `Close()` is a special case that combines I/O + external call. It may block on protocol teardown (QUIC connection drain), CGo cleanup (CFRunLoop stop), buffered write flush, or internal wg.Wait(). Always extract the reference under lock, nil the field, release the lock, then close.
```go
// WRONG: prov.Close() blocks on dnsOverride CGo
e.mu.Lock()
e.prov.Close()
e.prov = nil
e.mu.Unlock()

// RIGHT: nil under lock, close outside
e.mu.Lock()
prov := e.prov
e.prov = nil
e.mu.Unlock()
if prov != nil {
    prov.Close()  // no lock held, may block safely
}
```

**5. No defer Unlock() across large scope** — `defer mu.Unlock()` at the top of a function makes the critical section the entire function body. As the function grows, new code added inside silently inherits the lock. Use explicit Unlock() at the earliest safe point.
```go
// WRONG: any future edit to this function is under lock
func (c *Client) connect(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()  // hundreds of lines under lock
    // ... 50 lines of setup, I/O, error handling ...
}

// RIGHT: narrow critical sections with explicit unlock
func (c *Client) connect(ctx context.Context) error {
    c.mu.Lock()
    if c.closed { c.mu.Unlock(); return ErrClosed }
    c.mu.Unlock()
    // ... I/O with no lock ...
    c.mu.Lock()
    c.conn = newConn
    c.mu.Unlock()
}
```

Exception: `defer mu.Unlock()` is fine for short functions (≤10 lines) that only read/write fields.

### Accepted Exceptions

**slog under state lock** — `slog.Info/Warn/Debug` calls under state lock are permitted. The stdlib slog handler uses its own internal lock with no application-lock dependency. This exception holds ONLY because the project uses stdlib handlers. If a custom `slog.Handler` is ever registered, it MUST NOT acquire any application mutex. This exception does NOT extend to other logging libraries or `log.Printf`.

### Safe Patterns

**Read-copy-act**: Lock, copy references/build snapshot, unlock, act on copies.
```go
e.mu.Lock()
handler := e.handler
status := e.buildStatusLocked()
e.mu.Unlock()
if handler != nil { handler.OnStatus(status) }
```

**Check-release-act-reacquire**: Lock, check preconditions, unlock, do expensive work, re-lock, re-validate, store result.
```go
c.mu.Lock()
if c.conn != nil { c.mu.Unlock(); return c.conn, nil }
c.mu.Unlock()
conn := expensiveDial()  // no lock
c.mu.Lock()
if c.closed { c.mu.Unlock(); conn.Close(); return nil, ErrClosed }
c.conn = conn
c.mu.Unlock()
```

**Extract-nil-release-close**: Lock, save reference, nil field, unlock, close outside.
```go
e.mu.Lock()
prov := e.prov; e.prov = nil
tm := e.tm;     e.tm = nil
e.mu.Unlock()
if prov != nil { prov.Close() }
if tm != nil   { tm.Close() }
```

### Review Checklist (for any PR touching deadlock.Mutex / sync.WaitGroup)

- [ ] Is every Lock()→Unlock() section a pure read/write of struct fields?
- [ ] Does any Lock() scope call a method on another struct, interface, or closure?
- [ ] Does any Lock() scope do I/O (network, CGo, file, DNS, TLS, QUIC)?
- [ ] Does any Lock() scope call Close() on any resource?
- [ ] Does any Lock() scope wait on a goroutine (wg.Wait, <-chan)?
- [ ] Does any `defer mu.Unlock()` span more than 10 lines?
- [ ] Is every new mutex added to the Lock Ordering Graph?
- [ ] Is every serialization lock acquired WITHOUT holding any state lock?

## Error Ownership Rules (Constitutional — never violate)

### Core Principle

**The producer classifies; the consumer queries an interface.**

The code that creates an error has full context about what failed. The code that receives an error should never reverse-engineer classification by enumerating concrete types. If a consumer needs to distinguish error categories, the producer must implement a classification interface.

### The Rule

When a function returns an error, it must classify that error at the point of creation — not leave it to callers to pattern-match. Callers check classification via interface assertion (`errors.As` on a behavior interface), never via type enumeration or string matching.

```go
// WRONG: consumer enumerates types (breaks when new error types are added)
func isProxyLayerError(err error) bool {
    var se *StreamError
    if errors.As(err, &se) { return true }
    if errors.Is(err, ErrStreamRejected) { return true }
    return false // new error types silently misclassified
}

// RIGHT: producer marks, consumer asks interface
type proxyLayerErr interface { IsProxyLayer() bool }

func isProxyLayerError(err error) bool {
    var pe proxyLayerErr
    return errors.As(err, &pe) && pe.IsProxyLayer()
}
```

### Why This Matters

A consumer-side whitelist is correct on the day it's written. When a new error type is added (by a different developer, months later), the whitelist silently misclassifies it — defaulting to the wrong category. The compiler gives no warning. The bug manifests as incorrect fallback behavior under production load.

### Review Checklist

- [ ] Does any error consumer switch on concrete error types to decide behavior?
- [ ] Does every error classification have a corresponding interface implemented by the producer?
- [ ] Does `errors.As` traverse the full Unwrap chain correctly?

## Encapsulation Rules (Constitutional — never violate)

### Core Principle

**Never write another struct's fields. Call its methods.**

A struct owns its fields — including `atomic` fields and unexported state. External code must interact through methods, even for simple operations like resetting a counter. `atomic` provides concurrency safety, not encapsulation safety.

### The Rule

Do not directly read or write fields on another struct, even if they are exported or atomic. Instead, the owning struct exposes methods that encapsulate the operation.

```go
// WRONG: TransportManager directly writes QUICClient's internal atomic
func (tm *TransportManager) recordQuicSuccess() {
    if qc, ok := tm.quic.(*QUICClient); ok {
        qc.handshakeFailStreak.Store(0) // reaches into QC internals
    }
}

// RIGHT: QUICClient exposes a method
func (c *QUICClient) RecordSuccess() {
    c.handshakeFailStreak.Store(0)
}
func (tm *TransportManager) recordQuicSuccess() {
    if qc, ok := tm.quic.(*QUICClient); ok {
        qc.RecordSuccess()
    }
}
```

### Why This Matters

When a field is renamed, its semantics change, or additional side effects are needed (logging, metrics), the compiler finds all method call sites but NOT all direct field accesses from external structs. The owning struct is the single authority on how its state is mutated.

### Review Checklist

- [ ] Does any code outside a struct directly `.Store()` / `.Load()` on that struct's atomic fields?
- [ ] Does any code outside a struct directly assign to that struct's fields?
- [ ] Could the operation be encapsulated in a method on the owning struct?

## Layer Boundary Rules (Constitutional — never violate)

### Core Principle

**Lower layers do not know upper layers exist. Upper layers use lower layers through interfaces, not concrete types.**

The layer model (Platform Shell → Engine → Core → Provider → Wire) defines a strict dependency direction. Each layer depends only on the layer directly below it, through interfaces defined in the lower layer's package.

### The Rule

1. **No upward dependencies**: A lower-layer package must never import an upper-layer package. Wire must not import engine. Core must not import daemon.
2. **No concrete type assertions on layer interfaces**: When a layer receives an interface from below (e.g., `wire.Dialer`), it must not type-assert to the concrete implementation (e.g., `*QUICClient`) to access capabilities beyond the interface contract.
3. **Optional capabilities use registered interfaces**: If an upper layer needs an optional capability from a lower layer, the lower layer defines and documents the capability interface (e.g., `StatsProvider`). The upper layer type-asserts to the *interface*, not the concrete type.

```go
// WRONG: upper layer type-asserts to concrete type to read stats
if qc, ok := tm.primary.(*QUICClient); ok {
    stats := qc.WireStats()
}

// RIGHT: lower layer defines capability interface
type StatsProvider interface {
    WireStats() WireStats
}
// upper layer asserts on interface
if sp, ok := tm.primary.(StatsProvider); ok {
    stats := sp.WireStats()
}
```

### Registered Optional Interfaces

All optional capability interfaces that upper layers may type-assert on must be listed here:

| Interface | Package | Implemented by | Asserted by |
|-----------|---------|----------------|-------------|
| `StatsProvider` | wire | QUICClient, TCPWSClient | engine (stats collection) |
| `EchoProber` | wire | QUICClient | engine (echo health probe) |

When adding a new optional interface, add it to this table.

### Why This Matters

Concrete type assertions create hidden coupling. If a third transport is added (or a mock in tests), every assertion site silently becomes a no-op — the code compiles but the behavior changes. Interface assertions fail explicitly and are discoverable via `grep`.

### Review Checklist

- [ ] Does any code type-assert a layer interface to a concrete type from the layer below?
- [ ] Are all optional capability interfaces listed in the Registered Optional Interfaces table?
- [ ] Does any lower-layer package import an upper-layer package?

## Layer Docs (read on demand)

```
sniff/CLAUDE.md                Protocol sniffing (TLS/QUIC/HTTP/BT/STUN/DTLS detectors)
rule/                          In-tree routing engine: domain suffix + IP range binary search, k2b mmap format, inline/remote rule sets, connection-metadata matching (no CLAUDE.md — start at rule/engine.go)
cmd/k2/CLAUDE.md               Client CLI subcommands + dispatch
cmd/k2s/CLAUDE.md              Server CLI + foreground/service mode
cmd/k2r/CLAUDE.md              Gateway CLI subcommands + dispatch (Linux only)
config/CLAUDE.md               ClientConfig + ServerConfig + YAML formats
engine/CLAUDE.md               Unified tunnel lifecycle manager (state machine, EventHandler, Config assembly)
core/CLAUDE.md                 L4 ClientTunnel, Router, ConnectionHandler
core/dns/CLAUDE.md             DNS 13-step middleware pipeline
daemon/CLAUDE.md               Desktop platform shell: HTTP API, service management, auto-reconnect
gateway/CLAUDE.md              Gateway platform shell: k2r binary, TPROXY, LAN interception
webui/CLAUDE.md                Shared webapp embed + storage + platform + OTA upgrade (k2r + k2 Linux desktop)
provider/CLAUDE.md             Provider interface (TUN, Proxy, TProxy, iOS, Android)
server/CLAUDE.md               Server listener orchestration, TCP/UDP handlers
wire/CLAUDE.md                 k2v5 wire protocol, Dialer/Listener interfaces
wire/k2v5-protocol-spec.md    k2v5 protocol specification (TLS, ECH, camouflage, framing)
wire/VOCABULARY.md             Three-layer vocabulary convention (Wire/Relay/Tunnel)
wire/k2cc/CLAUDE.md            K2CC Vivace congestion control
appext/CLAUDE.md               App extension shell: gomobile SDK (macOS sysext, iOS, Android)
k2p/                           P2P home access — k2s behind NAT + ICE hole punching, data plane is k2v5 (no CLAUDE.md — see docs/superpowers/specs/2026-03-24-k2p-e2e-design.md)
signal/                        k2p signal/relay server — zero-trust, cannot decrypt or impersonate (no CLAUDE.md — start at signal/store.go)
probe/                         Wire transport probe registry + service (handshake + echo health) (no CLAUDE.md — start at probe/service.go)
safego/                        Panic-safe goroutine launcher (Go() wraps recover + slog)
test/CLAUDE.md                 E2E integration tests
docs/features/                 Feature specs (cloud-webapp, mobile-sdk, private-ip-guard, etc.)
docs/knowledge/                Distilled patterns (architecture, bugfix, gotchas, testing, tasks)
docs/contracts/                API contracts (webapp-daemon-api)
scripts/                       Install scripts (install-k2s.sh, install-k2.sh) + release pipeline (release.sh)
```

## Architecture

**L4 Proxy Model**: Client uses gVisor stack (sing-tun) to convert TUN packets to L4 TCP/UDP connections. Server is simple userspace proxy (no TUN/NAT).

### Layer Model

```
┌─────────────────────────────────────────────────────────┐
│ Platform Shell (daemon/ appext/ gateway/)                │
│   职责: 平台入口 + Config 组装 + 平台特有功能              │
│   规则: 不管隧道状态——状态由 engine 管, shell 只透传        │
├─────────────────────────────────────────────────────────┤
│ Engine (engine/)                                        │
│   职责: 唯一的隧道生命周期管理者 (single source of truth)  │
│   拥有: state machine, EventHandler, error classification │
│   组装: wire → provider → tunnel → DNS middleware         │
├─────────────────────────────────────────────────────────┤
│ Core (core/ + core/dns/)                                │
│   职责: L4 数据面 + DNS pipeline (无状态, 只处理连接)      │
├─────────────────────────────────────────────────────────┤
│ Provider (provider/)                                    │
│   职责: 平台 packet I/O 抽象 (TUN/Proxy → L4 connections)│
├─────────────────────────────────────────────────────────┤
│ Wire (wire/)                                            │
│   职责: k2v5 协议传输 (Dialer/Listener)                  │
└─────────────────────────────────────────────────────────┘
```

### Platform Call Chains

```
Desktop:   CLI → HTTP → daemon.Daemon ──[builds Config]──→ engine.Engine.Start()
App ext:   Native → appext.Engine ──[builds Config]──→ engine.Engine.Start()
Gateway:   CLI → HTTP → gateway.Gateway ──[builds Config]──→ engine.Engine.Start()
                                                           ↓
                                                      engine assembles:
                                                      rule → wire → provider → tunnel → DNS
```

Platform Shell (daemon/appext/gateway) assembles `engine.Config` with platform-specific fields (DirectDialer, NetworkMonitor for desktop; FileDescriptor for appext; TPROXY for gateway), then delegates all lifecycle to engine. Engine doesn't know or care which platform called it.

### Interfaces

- `wire.Dialer`: DialTCP, DialUDP, DialStreamUDP, Reset, Close (implemented by QUICClient, TCPWSClient, TransportManager, SwappableDialer)
- `wire.Listener`: AcceptTCP, ServeUDP, Close (implemented by QUICServer, TCPWSServer)
- `wire.DirectDialer`: DialContext, ListenPacket (implemented by daemon.directDialer, appext.protectedDialer)
- `wire.DialerController`: ControlFunc() (optional, for DNS socket protection — implemented by appext.protectedDialer)
- `wire.StatsProvider`: WireStats() (optional, type-asserted on Dialer — implemented by QUICClient, TCPWSClient)
- `provider.Provider`: Start(ctx, ConnectionHandler), Close, Name (TUN/Proxy/TProxy/iOS/Android)
- `provider.ConnectionHandler`: HandleTCP(conn, dest), HandleUDP(conn, dest) (implemented by ClientTunnel, dnsHandler)
- `engine.EventHandler`: OnStatus(Status), OnStats(tx, rx) (implemented by daemon, appext.eventBridge)
- `engine.NetworkChangeNotifier`: Start(callback func()) error, Close() error (implemented by daemon.singTunMonitor, appext.nePlatform)
- `dns.DNSQuerier`: Query(ctx, *dns.Msg) (*dns.Msg, error) (implemented by DirectDNSClient, ProxyDNSClient)

### Binary Split

- `k2`: client (CLI, daemon, provider, core, DNS middleware) — Linux/Windows desktop
- `k2s`: server (listener, auth, TCP/UDP handlers)
- `k2r`: gateway (CLI, gateway/, TPROXY interception) — Linux routers/soft-routers
- `appext/`: gomobile SDK (Engine + EventHandler bridge — gomobile can't export dependency interfaces) — macOS sysext, iOS, Android
- Shared: wire/, config/, engine/

## Data Flow

- DNS (all TUN): App → TUN → gVisor → dnsHandler (port 53 TCP+UDP) → Middleware.HandleQuery → Cache/Filter/rule/dedup → Upstream
- Traffic: TUN → gVisor stack → ConnectionHandler → Router → Wire (QUIC/TCP-WS) → Server
- Server: Wire → Auth → AcceptTCP/ServeUDP → net.Dial → Target
- Control: CLI → HTTP 127.0.0.1:1777 → Daemon → Engine

## Resource Ownership Rules

> **谁创建，谁销毁。Borrower 只用不关。**

Engine creates provider, transport, and tunnel during `Start()`. Engine closes them during `Stop()`. Tunnel borrows provider and dialer references but **never closes them** — it only manages its own resources (context, WaitGroup, connTracker).

```
engine.Stop()
  ├─ healthMonitor.stop()// Engine owns health monitor → stop goroutine first (reads tm/tunnel)
  ├─ prov.Close()       // Engine owns provider → Engine closes it
  ├─ tm.Close()         // Engine owns transport → Engine closes it
  └─ tunnel.Close()     // Tunnel only drains its own goroutines
     ├─ cancel()        // Tunnel owns context
     ├─ conns.closeAll()// Tunnel owns connTracker
     └─ wg.Wait()       // Tunnel owns WaitGroup
```

| Resource | Owner (create + destroy) | Borrower (use only) |
|----------|--------------------------|---------------------|
| provider | Engine | Tunnel, dnsHandler |
| transport/dialer | Engine | Tunnel |
| healthMonitor | Engine | — |
| ctx + wg + connTracker | Tunnel | — |
| handler goroutines | Tunnel | — |
| networkMonitor | Engine | — |

**Why this order matters**: Health monitor stops first (its goroutine reads tm and tunnel). Provider closes next (stops new TUN packets), then transport (drops wire connections), then tunnel (drains in-flight handlers). Tunnel's `connTracker.closeAll()` force-closes any connections stuck in `pipe()`/`pipePacket()` so `wg.Wait()` returns promptly.

**Violation pattern to avoid**: `tunnel.Close()` must never call `provider.Close()` or `dialer.Close()`. This causes double-close panics (e.g. sing-tun `StopFD.Stop()` on macOS).

## Critical Gotchas

- **connectMu hard timeout**: `QUICClient.connect()` wraps `transport.Dial` in a goroutine with `quicDialHardTimeout` (15s). If quic-go's internal `Conn.destroy()` blocks (waiting for `conn.run()` exit on context cancel), the goroutine is orphaned and connectMu is released. Without this, connectMu starvation blocks all DialTCP/DialUDP for 30s+ (detected in v0.4.1 production, 4/22 devices). `TCPWSClient` uses `net.Dialer{Timeout}` for kernel-level TCP dial enforcement. `daemon.directDialer` derives Timeout from context deadline.
- **Race test timeout**: `go test -race ./wire/...` needs `>300s` (arc integration benchmarks are slow). Use `-timeout 300s` or `-short` to skip long-running tests.
- QUIC stream `Close()` only sends FIN (write direction). Must also call `CancelRead()` to release stream slot — `quicStreamConn.Close()` handles both
- sing-tun gVisor stack owns connections — handler must defer close, not close prematurely
- `ReadStreamHeader`/`ReadStreamResponse` require `io.ByteReader` — wrap `net.Conn` with `bufio.NewReader(conn)`
- `net.Pipe()` is synchronous — writes block until reads happen on other end
- TUN device requires root. `k2 setup` or `k2 service install` installs daemon as root via launchd/systemd/Windows Service
- QUIC datagrams may not be supported — wire.DialUDP falls back to stream-based UDP
- `blockQUIC` defaults to `false` — browser QUIC flows through DATAGRAM path (unreliable, no retransmission at tunnel layer). Inner browser QUIC CC (BBR/Cubic) adapts to outer K2CC throughput. Set `block_quic: true` in client config to restore ICMP-reject behavior.
- `SendDatagram` uses atomic inflight counter (`maxDatagramInflight=32`) — when concurrent calls exceed limit, excess packets dropped immediately (no goroutine spawned). Protocol-correct for unreliable DATAGRAM. Inner protocol retransmits as needed.
- smux needs buffer tuning for VPN traffic (default 4MB too small, use 16MB)
- ECH requires X25519 keys (not P-256) — Go's crypto/tls only supports DHKEM(X25519)
- DNS queries need trailing dot (FQDN) — `crypto.cloudflare.com.` not `crypto.cloudflare.com`
- TLS wire formats are big-endian — always use `binary.BigEndian` for ECH/TLS parsing
- Base64 in URLs must use `base64.URLEncoding` — standard `+` decoded to space by `url.Query()`
- uTLS forces outer SNI = ECH config's `public_name` — cannot be overridden independently
- Server generates dual certs (RSA+EC) — both padded to match target site's cert length
- EC CA (`wire/kaitu_ca.pem`, disguised as DigiCert) can sign both RSA and EC server certs
- `tun_desktop.go` build tag: `(darwin && !ios) || (linux && !android) || windows` — must exclude android/ios
- gateway provider (k2r) manages nftables/iptables TPROXY rules — cleanup on Close() is critical
- `tun.Options.DNSServers` must NOT be set — it configures OS DNS settings, not packet interception. sing-tun auto-derives `TUN_IP.Next()`. DNS hijack is application-level (dnsHandler checks port 53)
- `ProviderConfig.RouteExcludeAddrs` (NOT DNSServers) excludes IPs from TUN routing — prevents DirectDNSClient query loops

## Dependencies

- `rule/` (in-tree) — multi-ruleset routing engine: domain suffix binary search + IP range O(log n) binary search, k2b mmap-indexed format, routes-based config, per-service IP-CIDR + domain rule sets, inline rules (domain_suffix/ip_cidr in config), connection metadata matching (process_name, package_name, network, ip_is_private)
- `github.com/miekg/dns` — DNS message handling
- `github.com/sagernet/sing-tun` — TUN device with gVisor stack
- `github.com/apernet/quic-go` — QUIC with congestion control API
- `github.com/metacubex/utls` — TLS fingerprinting + ECH
- `github.com/xtaci/smux/v2` — TCP-WS stream multiplexing
- `golang.org/x/mobile` — gomobile bind (iOS xcframework + Android aar)

## TODOs

Tracked in `docs/todos/` with priority prefixes:

```
docs/todos/p0-*.md     Critical / next up
docs/todos/p1-*.md     Important
docs/todos/p2-*.md     Nice to have
```

## Status

**Done**:
- L4 architecture, wire protocol (QUIC+TCP-WS), DNS middleware (L3+L4, stale-while-revalidate, persistent UDP pooling)
- Auth, ECH (DNS-based forging + config rotation), TLS record padding, dual certs (RSA+EC), CA pool (system + Kaitu CA - Chinese CAs)
- PCC (K2CC Vivace congestion control with AIMD-ε, ackRate cwnd compensation)
- Client tunnel, server handlers, e2e tests, zero-config server startup
- k2/k2s binaries (k2s interactive wizard + upgrade command + beta channel)
- appext SDK (gomobile, macOS sysext+iOS+Android, iOS GOGC=10 memory optimization)
- k2r gateway binary (cmd/k2r, gateway/ package, TPROXY with nftables/iptables auto-detect)
- Wire observability (K2CC metrics + TX/RX counting + HealthMonitor FSM + TCP health gate + background QUIC probe)
- Protocol sniffing (TLS/QUIC/HTTP/BT/STUN/DTLS + QUIC blocking with ICMP Port Unreachable)
- Happy Eyeballs (RFC 8305), transport behavior contract tests (20 scenarios in wire/ + engine/)
- Quality gates (`make quick-check` / `make pre-release`), goleak goroutine leak detection
- safego package (panic-safe goroutine launcher), daemon doUp abort mechanism
- CLI redesign (pure subcommand dispatch, --json output, non-interactive k2s setup)
- k2subs:// subscription URLs (weighted server selection, background refresh, disk cache)
- Per-outbound transport (each k2v5 route gets independent TransportManager)
- Unified release pipeline (LATEST + checksums.txt, S3 upload, install scripts, CI)

## Platform Mapping

| Platform | Entry | Shell | Provider | DirectDialer | NetworkMonitor |
|----------|-------|-------|----------|--------------|----------------|
| macOS sysext | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| iOS | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| Android | aar | appext/ | tun (FD) | protectedDialer | nil (OS calls OnNetworkChanged) |
| Linux desktop | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| Windows | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| Linux gateway | cmd/k2r | gateway/ | gateway (TPROXY) | nil | nil |
