# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux fallback).
Three binaries — `k2` (client: CLI + daemon with HTTP API on 127.0.0.1:1777; Linux/Windows/macOS desktop — macOS runs it as a launchd service, `cmd/k2/service_darwin.go`), `k2s` (server), `k2r` (LAN gateway, Linux only) — plus `appext/` (gomobile SDK for iOS + Android). In-tree in k2app since 2026-10-29 (subtree, path `k2/`); go module is still `github.com/kaitu-io/k2`. This file loads for every k2 change — only cross-package rules live here; package detail is in the package's own `CLAUDE.md`.

## Quick Commands

```bash
go build -o k2 ./cmd/k2 && go build -o k2s ./cmd/k2s && GOOS=linux go build -o k2r ./cmd/k2r
go test -short ./...                 # macOS: linux-only packages (gateway/, webui/, daemon *_linux.go) are NOT COMPILED — not SKIP, absent
GOOS=linux go vet ./gateway/... ./webui/... ./daemon/...         # cross-check linux-only code from macOS (or run tests in a golang:1.26 container)
go test ./lint/                      # REQUIRED before every k2 commit — standalone AST package (concurrency + DIAG registry); testing only the changed package misses it
go test -race -timeout 300s ./wire/...   # race runs need ≥300s
make gateway-check / make gateway-uat    # gateway vet+build+unit / Docker UAT (k2/Makefile)
make appext-ios / make appext-android    # run from the k2app ROOT Makefile: its appext-android adds -extldflags=-Wl,-z,max-page-size=16384 (16 KB pages, Play requirement); k2/Makefile's same-named target does not
```

**Pre-merge gate = the labtest `k2` suite** (root `.labtest.yml` → `scripts/ci/k2ci-run.sh suite_k2`: linux `go test ./...` incl. `lint/`, `-tags=deadlock_disable` build, `third_party/smux` race, krs.tar.gz size). Not `make pre-release` / `make quick-check` — nothing in CI calls them. k2s releases go through `.github/workflows/release-k2s.yml` on a `v*-k2s` tag (manifest-based, see `cmd/k2s/upgrade.go`), not `make release`.

## Code Style

- **Minimalist**: avoid over-engineering, prefer concrete types over interfaces. **Stdlib first**: `os.Args` + `flag.NewFlagSet` for CLI, `net/http` for HTTP.
- **Error wrapping**: always `fmt.Errorf("context: %w", err)`.
- **Trust protocol semantics**: when QUIC/TLS/smux provide keepalive, idle timeout, liveness — use them; don't layer app-level health checks on top (that duplication caused "server can't reach destination" ≡ "transport broken" misclassification bugs).
- **Deadlock detection**: mutexes are `deadlock.Mutex`/`deadlock.RWMutex` (`github.com/sasha-s/go-deadlock`). **Desktop production keeps detection ON** — `DESKTOP_TAGS=-tags=release` (no `deadlock_disable`), `DeadlockTimeout` 60s, on detection DNS cleanup then `os.Exit(2)` and the service manager restarts (`cmd/k2/deadlock_prod.go`). Only mobile builds (`MOBILE_TAGS`/gomobile) add `deadlock_disable`. Tests: 5s. A lock legitimately held across blocking I/O therefore must NOT be a `deadlock.Mutex` (it would false-fire and kill the daemon) — the few deliberate `sync.Mutex` uses carry a comment saying why (`wire/quic.go`, `wire/tcpws.go` writeMu/initMu, `core/dns/middleware.go` epochMu, `core/dns/hosts.go`). Every other new mutex: `deadlock.Mutex`. `WaitGroup`/`Once`/`Pool` stay stdlib.

## Diagnostic Logging (DIAG) — never violate

All diagnostic logs use `log/slog`. The `DIAG:` prefix is reserved for operational logs that enable field troubleshooting without DEBUG.

| Layer | Level | Trigger | grep |
|-------|-------|---------|------|
| 1. Heartbeat | INFO | every 30s, `healthMonitor.sample()` | `grep "DIAG: heartbeat"` |
| 2. Events | INFO/WARN | on anomaly, threshold-gated | `grep "DIAG:" \| grep -v heartbeat` |
| 3. Full trace | DEBUG | all operations | switch at runtime: `curl -X POST http://127.0.0.1:1777/api/log-level -d '{"level":"debug"}'` |

- **L1**: fixed message `"DIAG: heartbeat"`, every 30 samples — never 1Hz. Field list + semantics (which fields are cumulative vs per-window, what `dialFail` with `dialOk=0` means) are in `docs/diag-events.md`; adding/removing a field = update it there.
- **L2**: message is exactly `"DIAG: <event-name>"` (kebab-case, 2-3 words); fields answer "where" + "why" (domain, addr, transport, err, latency); INFO for observations, WARN for failures; threshold-gated — never log every occurrence.
- **L3**: don't touch existing DEBUG logs; new code uses DEBUG for per-operation detail, DIAG for threshold-gated observations.
- **Registry**: every event has a row in **`k2/docs/diag-events.md`** (level, file, meaning/fields, grep playbook, removed events). **A new, renamed, or removed event must be registered there in the same commit — `lint/diag_events_test.go` fails in both directions** (code without row, row without code).

## Concurrency Rules — never violate

**A critical section is a pure function of in-memory state**: between `Lock()` and `Unlock()` only reads/writes of the fields that mutex protects. Go mutexes are non-reentrant with no ownership tracking; go-deadlock is a runtime safety net, not a design — a deadlock path that exists will be reached.

- **State lock** (`mu`): protects fields, held for microseconds. **Serialization lock** (`writeMu`, `connectMu`, `opMu`, …): serializes a non-thread-safe resource and may intentionally span I/O, but must NEVER be acquired while holding a state lock; its ordering must be documented.

Forbidden under a **state lock**:
1. **External calls** — interface methods, other structs' methods, callbacks, closures (`handler.OnStatus()`, `resource.Close()`). Allowed: pure stdlib value funcs (`time.Now`, `fmt.Sprintf`) and same-struct field-only helpers. → *not lint-checked*
2. **I/O / unbounded work** — network, DNS, TLS/QUIC handshake, `OpenStreamSync`, CGo/FFI (SCDynamicStore, CFRunLoop), `exec.Command`, file I/O. → *not lint-checked*
3. **Waiting** — `wg.Wait()`, `<-chan`, `Cond.Wait()`, any blocking receive (deadlock if the waited goroutine needs the lock). → *not lint-checked*
4. **`Close()`** — extract the reference, nil the field, unlock, then close. → **lint** `TestConcurrencyLint_CloseUnderLock`
5. **`defer mu.Unlock()` in a function body longer than 15 lines** — use explicit Unlock at the earliest safe point. → **lint** `TestConcurrencyLint_DeferUnlockScope`

**The lint (`lint/concurrency_lint_test.go`) only sees #4 and #5, and only for a lock field named exactly `mu`** — `stateMu`, `connMu`, a local `sync.Mutex` and all of #1-#3 plus lock ordering are review-only. Exemption: a comment containing `// concurrency:allow — <reason>` on the `Lock()`/`defer` line or up to 3 lines above it (e.g. `wire/quic.go`, `core/dns/proxy.go`).

Accepted exception: `slog.*` under a state lock is fine — only while every registered `slog.Handler` is stdlib (a custom handler must never take an app mutex); does not extend to other loggers. Safe shapes: **read-copy-act** (snapshot under lock, act outside), **check-release-act-reacquire** (re-validate after re-locking), **extract-nil-release-close**.

### Lock ordering

Acquire in this order, never reverse. **Any lock nesting that crosses a package boundary must be registered here**; package-internal orders live in that package's `CLAUDE.md` (wire, gateway, provider, config, rule, daemon).

```
engine.mu (state)
  → healthMonitor.mu (state)
  → daemon.mu (state) → daemon.sseMu (state)
  → wire.TransportManager.mu (state) → QUICClient.mu / TCPWSClient.mu (state)
  → provider dnsOverride.mu (state)
  → core connTracker.mu (state)
wire serialization locks (QUICClient.connectMu / initUDPMu, …) are never taken under any lock above — see wire/CLAUDE.md
```

No package doc yet for these: `signal.Store.mu` (RWMutex) → `PeerEntry.WriteMu` (serialization); `signal.SessionRouter.mu`; `signal` `wsConn.writeMu` and `k2p.CandidateExchange.writeMu` (serialization, standalone, guard `ws.WriteJSON`); `core` `k2pOutbound.connectMu` (serialization) → `k2pOutbound.mu`. None nest with engine locks.

## Error Ownership — never violate

**The producer classifies; the consumer queries an interface.** The code that creates an error marks its category (e.g. implements `IsProxyLayer() bool`); callers check via `errors.As` on that behavior interface — never by enumerating concrete types or matching strings. A consumer-side whitelist silently misclassifies the next new error type (no compiler warning; shows up as wrong fallback behavior under load). Make sure the marker survives the `Unwrap` chain.

## Encapsulation — never violate

**Never write (or `.Store()`/`.Load()`) another struct's fields — call its methods**, even for exported or `atomic` fields (`atomic` gives concurrency safety, not encapsulation). The owning struct is the single authority on how its state mutates; the compiler finds method call sites on a rename/semantic change, not direct field accesses.

## Layer Boundaries — never violate

Layers: Platform Shell → Engine → Core → Provider → Wire.
1. **No upward imports** (wire must not import engine; core must not import daemon).
2. **No type assertion of a layer interface to a lower layer's concrete type** (`tm.primary.(*QUICClient)`) — with a third transport or a test mock the assertion silently becomes a no-op.
3. **Optional capabilities use a registered interface** defined in the lower package; the upper layer asserts on the interface. Registered: `wire.StatsProvider` (QUICClient, TCPWSClient → engine stats), `wire.EchoProber` (QUICClient, TCPWSClient → engine echo probe). **Add new ones to this list.**

## Resource Ownership

**谁创建，谁销毁。Borrower 只用不关。** Engine creates provider/transport/tunnel in `Start()` and closes them in `Stop()`; tunnel borrows provider and dialer and never closes them (double-close panics, e.g. sing-tun `StopFD.Stop()`). Stop order + owner table: `engine/CLAUDE.md`.

## Architecture

**L4 Proxy Model**: client uses the gVisor stack (sing-tun) to turn TUN packets into L4 TCP/UDP connections. Server is a userspace proxy — no TUN, no kernel NAT table; its UDP egress socket is unconnected (`net.ListenUDP`, full-cone) with a per-session destination LRU (`server/handler.go`) — NAT-*like* addressing, not a NAT (see `server/CLAUDE.md`).

```
┌─────────────────────────────────────────────────────────┐
│ Platform Shell (daemon/ appext/ gateway/)                │
│   职责: 平台入口 + Config 组装 + 平台特有功能              │
│   规则: 不管隧道状态——状态由 engine 管, shell 只透传        │
├─────────────────────────────────────────────────────────┤
│ Engine (engine/)                                        │
│   职责: 唯一的隧道生命周期管理者 (single source of truth)  │
│   拥有: state machine, EventHandler, error classification │
│   组装: rule → wire → provider → tunnel → DNS middleware  │
├─────────────────────────────────────────────────────────┤
│ Core (core/ + core/dns/)                                │
│   职责: L4 数据面 + DNS pipeline (无状态, 只处理连接)      │
├─────────────────────────────────────────────────────────┤
│ Provider (provider/)                                    │
│   职责: 平台 packet I/O 抽象 (TUN/Proxy → L4 connections)│
│   (gateway TPROXY provider lives in gateway/)           │
├─────────────────────────────────────────────────────────┤
│ Wire (wire/)                                            │
│   职责: k2v5 协议传输 (Dialer/Listener)                  │
└─────────────────────────────────────────────────────────┘
```

```
Desktop:   CLI → HTTP → daemon.Daemon   ──[builds engine.Config]──→ engine.Engine.Start()
App ext:   Native → appext.Engine        ──[builds engine.Config]──→ engine.Engine.Start()
Gateway:   CLI → HTTP → gateway.Gateway ──[builds engine.Config]──→ engine.Engine.Start()
```

The shell fills the platform fields (DirectDialer + NetworkMonitor on desktop, FileDescriptor on appext, its own provider on gateway) and delegates all lifecycle to engine; engine never knows which platform called it. Interfaces are documented where they are defined (`wire/`, `provider/`, `engine/` docs).

Data flow: DNS (TUN) = App → TUN → gVisor → dnsHandler (port 53 TCP+UDP) → `Middleware.HandleQuery` → upstream. Traffic = TUN → gVisor → ConnectionHandler → Router → Wire (QUIC/TCP-WS) → Server → Auth → `net.Dial` → target. Control = CLI → HTTP 127.0.0.1:1777 → Daemon → Engine.

### Platform Mapping

| Platform | Entry | Shell | Provider | DirectDialer | NetworkMonitor |
|----------|-------|-------|----------|--------------|----------------|
| macOS desktop | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| iOS | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| Android | aar | appext/ | tun (FD) | protectedDialer | nil (OS calls `NotifyNetEvent`) |
| Linux desktop | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| Windows | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| Linux gateway | cmd/k2r | gateway/ | gateway (TPROXY) | gatewayDirectDialer (SO_MARK) | nil |

## Layer Docs (read on demand)

Paths are relative to `k2/`. **`k2/docs/` and `k2/scripts/` are k2's own, not the k2app-root `docs/` / `scripts/`.**

```
sniff/CLAUDE.md          Protocol sniffing (TLS/QUIC/HTTP/BT/STUN/DTLS detectors)
rule/CLAUDE.md           In-tree routing engine: two-phase matching, match dimensions, criteria predicates, tmp pin LRU, bundle lifecycle
cmd/k2/ cmd/k2s/ cmd/k2r/ CLAUDE.md   Client / server / gateway CLIs
config/CLAUDE.md         ClientConfig + ServerConfig + Subscription + logging
engine/CLAUDE.md         Tunnel lifecycle: state machine, Start pipeline, error codes, health, resource ownership
core/CLAUDE.md           L4 ClientTunnel, Router, outbounds, connection handling
core/dns/CLAUDE.md       DNS middleware pipeline
daemon/CLAUDE.md         Desktop shell: HTTP API, service management, auto-reconnect
gateway/CLAUDE.md        k2r shell: TPROXY, LAN interception, control API auth
webui/CLAUDE.md          Shared webapp embed + storage + web OTA (k2r + k2 Linux desktop)
provider/CLAUDE.md       Provider interface (TUN, Proxy, mobile FD), process attribution
server/CLAUDE.md         Server listener orchestration, TCP/UDP handlers
wire/CLAUDE.md           k2v5 wire protocol, Dialer/Listener, transport selector — + wire/k2v5-protocol-spec.md, wire/VOCABULARY.md
wire/k2cc/CLAUDE.md      K2CC Vivace congestion control
appext/CLAUDE.md         gomobile SDK shell (iOS, Android)
test/CLAUDE.md           E2E integration tests; test/netem/ lossy-link emulation
lint/                    AST guards run by `go test ./lint/` (concurrency rules #4/#5, DIAG registry) — no CLAUDE.md
subscription/            k2subs:// session manager (implements engine.OutboundProvider) — no CLAUDE.md, start at subscription/manager.go
k2p/ signal/             P2P home access + its zero-trust signal server — no CLAUDE.md (docs/superpowers/specs/2026-03-24-k2p-e2e-design.md, signal/store.go)
probe/                   Transport probe registry + service — no CLAUDE.md, start at probe/service.go
safego/                  Panic-safe goroutine launcher (Go() wraps recover + slog)
docs/diag-events.md      DIAG event registry (lint-enforced)
docs/features/ docs/knowledge/ docs/contracts/   Feature specs, distilled patterns, webapp-daemon API contract
scripts/                 install-k2*.sh, release.sh (legacy manual pipeline), soak diff scripts
```

## Dependencies

- `kuic` (`github.com/kaitu-io/kuic`, QUIC core: private fork of `apernet/quic-go` with utls ClientHello fingerprinting + ECH + pluggable CC) and `smux` are **in-repo** at k2app-root `third_party/kuic` and `third_party/smux` via `go.mod` `replace` — change them with an ordinary commit; maintenance notes in `third_party/kuic/MAINTAINING.md` and `third_party/smux/K2_PATCHES.md`. The old `kaitu-io/k2` and `kaitu-io/kuic` repos are retired.
- Other key deps: `sagernet/sing-tun` (TUN + gVisor), `metacubex/utls` (TLS fingerprinting + ECH), `miekg/dns`, `golang.org/x/mobile` (gomobile bind).
