# Error Propagation Test Matrix — macOS Tauri Desktop (Daemon Mode)

**Date:** 2026-02-26
**Platform:** macOS, Tauri v2, daemon mode (HTTP :1777)
**Scope:** P0 — Error forward propagation from k2 engine to UI

## Error Chain Under Test

```
Engine ClassifyError → Daemon SSE (/api/events) → Rust status_stream.rs → Tauri event
→ transformStatus() (tauri-k2.ts) → VPN store (vpn.store.ts) → UI (CollapsibleConnectionSection)
```

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| T01 | P0 | Baseline | Connect to valid server | state=connected, UI shows "已连接" | PASS | JP 8292 connected, SSE: connecting→connected→reconnecting→connected (NWPathMonitor first-fire known) |
| T02 | P0 | Baseline | Disconnect normally | state=disconnected, UI shows "未连接" | PASS | SSE: vpn-status-changed: disconnected delivered |
| T03 | P0 | Error:503 | Connect to unreachable server (bad IP) | state=error, error.code=503, UI shows i18n error | FAIL:engine-dns-blocks-error | Root cause confirmed: DNS handler intercepts port 53 before tunnel HandleUDP. Wire broken → DNS fails silently → no TCP connections → handleTCPProxy never called → ReportWireError never called → engine stays "connected" forever. See Findings. |
| T04 | P0 | Error:408 | Connect to server that times out | state=error, error.code=408, UI shows i18n error | BLOCKED:T03 | Same root cause — DNS handler silently absorbs wire failures |
| T05 | P0 | Error:display | Error state shows i18n text (not raw message) | UI displays translated error, NOT raw engine message | BLOCKED:T03 | Cannot test — engine never emits error state |
| T06 | P0 | Service | Kill daemon while disconnected | service-state-changed: false, UI shows service error within 10s | PASS | launchd unloaded, SSE false cycling 3s, alert "服务连接失败" displayed after ~30s, "解决"/"更多" buttons visible |
| T07 | P0 | Service | Restart daemon after kill | service-state-changed: true, UI recovers to disconnected | PASS | launchd reloaded, SSE true, "Service recovered, resetting alert state", red banner cleared |
| T08 | P0 | SSE | SSE delivers status events in real-time | vpn-status-changed events appear in console during connect/disconnect | PASS | All state transitions logged: connecting, connected, reconnecting, disconnected. service-state-changed for daemon availability. |

## Findings

### CRITICAL: DNS handler silently blocks all error detection (T03) — CONFIRMED ROOT CAUSE

**Severity:** P0
**Symptom:** Engine reports `state: "connected"` indefinitely when wire server is unreachable. Tested with `ip=192.0.2.1` (RFC 5737 TEST-NET, drops all packets) and `ip=127.0.0.1:65534` (connection refused). No error state emitted after 120s+ in either case.

**Impact:** User sees "已连接" (connected, green circle) when nothing works. Error codes 503/408 never reach UI.

**Root cause (confirmed via code trace + live DNS testing):**

The error detection chain has a fatal gap at the DNS layer:

```
1. engine.Start() → TUN device up → state=connected (wire NOT tested)
2. System DNS set to 198.18.0.8 (TUN DNS handler)
3. All DNS queries → dnsHandler (port 53 intercept) → ProxyDNSClient → wire.DialTCP
4. Wire broken → ProxyDNSClient fails → DNS query returns SERVFAIL/timeout
5. No DNS resolution → no TCP connections created by apps
6. tunnel.handleTCPProxy() never called → ReportWireError() never called
7. Engine stays "connected" forever with no error
```

**Evidence (live DNS probing with TUN up + broken wire):**
```bash
dig +short example.com @198.18.0.8      # timeout (TUN DNS → broken wire)
dig +short example.com                    # timeout (system resolver → TUN DNS)
dig +short example.com @114.114.114.114   # 104.18.27.120 (direct, bypasses TUN)
dig +short example.com @8.8.8.8          # timeout (routed through TUN)
```

**Code path:**
- `k2/engine/engine.go:505-530` — `ReportWireError()` only called from tunnel handlers
- `k2/core/tunnel.go:219-235` — `handleTCPProxy()` calls `wireReporter.ReportWireError(err)` on DialTCP fail
- `k2/core/tunnel.go:261-277` — `handleUDPProxy()` same pattern, but port 53 is intercepted BEFORE reaching here
- `k2/engine/dns_handler.go` — DNS handler intercepts ALL port 53 traffic before tunnel's HandleUDP

**Chain break:** DNS handler → ✗ (silent failure) → tunnel.HandleTCP/UDP never called → ReportWireError never called → Engine → Daemon SSE → ... → UI

**Fix required (k2 submodule):** Engine needs wire health check independent of traffic. Options:
1. **Proactive wire probe**: Engine sends a test dial after TUN setup, reports error if wire unreachable
2. **DNS handler error propagation**: dnsHandler calls ReportWireError when ProxyDNSClient consistently fails
3. **Heartbeat/keepalive**: Periodic wire liveness check on QUIC/TCP-WS transport

### Additional discovery: k2v5 URL parameter precedence

During T03 testing, discovered that the `ip=` URL parameter (not `@host:port`) determines the actual wire connection target. The `ech=` parameter can also override the connection target via CDN routing. To simulate unreachable server, ALL of these must be controlled: `@host:port`, `ip=`, `ech=`, `pin=`, `hop=`.

### MINOR: TUN mode captures daemon loopback traffic

**Symptom:** `_k2.run('down')` during TUN+global mode failed with "error sending request for url (http://127.0.0.1:1777/api/core)". The TUN device captured the IPC request to the daemon itself.

**Impact:** Cannot disconnect from UI when connected in global mode with broken wire. Must use direct `curl` to daemon.

**Note:** This may be expected behavior — TUN global mode captures all traffic. The daemon should exclude its own listen address from TUN routing.

## Bridge Fix Status (commit 2f37def)

The `transformStatus()` fix in `tauri-k2.ts` and `capacitor-k2.ts` is **correct but untestable** — it handles `connected + error` → synthesize `"error"` state with `retrying` flag. However, the engine never emits `connected + error` because ReportWireError is never called (T03 root cause). Once the k2 engine fix is applied, the bridge code will work as designed.

## Summary

```
SCAN COMPLETE: 5 PASS, 1 FAIL, 0 SKIP, 2 BLOCKED
PASS:  T01 — Baseline connect (JP server, SSE transitions confirmed)
PASS:  T02 — Baseline disconnect (SSE delivered)
FAIL:  T03 — DNS handler silently blocks error detection (engine stays "connected" forever)
BLOCKED: T04 — Blocked by T03 (DNS handler root cause)
BLOCKED: T05 — Blocked by T03 (no error state to display)
PASS:  T06 — Service kill detected, alert displayed (~30s)
PASS:  T07 — Service restart recovery works
PASS:  T08 — SSE event delivery confirmed (all transitions)
```

## Next Steps

1. **k2 engine fix (submodule):** Implement wire health check — engine must detect wire failures independent of DNS/traffic. Recommended: DNS handler calls ReportWireError when ProxyDNSClient fails N consecutive queries.
2. **Re-test T03/T04/T05** after engine fix — bridge transformStatus() is ready.
3. **P1 tests:** Network transition (OnNetworkChanged), error recovery (ClearWireError), specific error codes (401, 403, 502).

---

# Relay 性能重构真机 Smoke — Android (9c7caffb, A14)

**Date:** 2026-07-13
**Platform:** Android 14, device 9c7caffb, io.kaitu debug build
**Scope:** relay 性能重构 A (k2 c12f42f / parent 6882b788) — cold-start priming, relay-fetch 提速(旧 30s→目标 sub-second), relay-add-nodes 摄入, 无回归
**Build note:** 从工作树编译 = A(已提交)+ B(未提交 krs.tar.gz 2.5MB + dep bump);relay 路径与 krs 正交,A+B build 验 A,顺带给 B 的 2.5MB embed 一个 Android 侧信号。
**Observe:** `adb logcat`(Go stderr→/dev/null,看 Capacitor/Console JS 侧日志)

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| AR01 | P0 | Cold-start | 全新安装冷启动,App 启动到主界面 | 无崩溃,到达 UI | PASS | 冷启动到 UI,无 crash/ANR(仅 monkey 启动器+高通 perf HAL 噪音) |
| AR02 | P0 | Priming | ensureSeeded 在首个云请求前把 relay-add-nodes 灌给 Go | logcat 见 relay-add-nodes 先于首次 relay-fetch | PASS | relayAddNodes@13.209 灌 5 seed 节点,先于所有 relayFetch@13.887+ — ensureSeeded 前置成立 |
| AR03 | P0 | Perf | relay-fetch 往返延迟(旧 30s) | 明显非 30s,sub-second/≤2s | PASS | 8 请求 ~65ms 突发(非串行);首个 relay 往返 ~1s(QUIC+ECH 冷握手);无 30s stall |
| AR04 | P0 | Correctness | 冷启动 relay-fetch 返回 code:0 | 非 502(空池)/非 -1(降级) | PASS | 8× Go DIAG status=200,0 transport-failed,0 code:-1,0 502 |
| AR05 | P1 | Ingestion | relay-add-nodes 并入 Go 池 added>0 | Go 报告 added 计数 | PASS | seed 节点 35.88.216.55 被 relay-fetch 实际选用 — 摄入端到端闭环 |
| AR06 | P1 | No-regression | 正常连接隧道 | tunnel connected | BLOCKED:device-disconnected | 需登录+设备;隧道连接走 _k2.run(up),非 relay-fetch 路径,与 A 正交 |
| AR07 | P2 | Logged-in | 登录态冷启动(force-stop+重开)relay 仍快 | 重启后 relay-fetch 快 | BLOCKED:device-disconnected | 设备 USB 掉线;relay 传输对认证无感(已见 /api/user/info 走 relay 返 401),登录后仅 401→200,同传输 |

---

# SP4 Release Regression — macOS proxy-mode (k2 e158935)

**Date:** 2026-08-04
**Platform:** macOS, k2 binary `0.4.8 (e158935)` built from feat/udp-game-wiring, **proxy mode** (no TUN, no root)
**Scope:** P0 regression after k2 submodule advance (SP1 full-cone UDP + SP2 port/protocol rules) + webapp type alignment
**Method:** QA daemon on `127.0.0.1:1778` (isolated from production daemon on 1777), real production node JP 5372 (18.182.139.255), real account session (hi@kaitu.io, membership extended for QA)

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| Q01 | P0 | Build | Binary version stamp | `0.4.8 (e158935)` | PASS | ldflags stamping verified against the shipped artifact |
| Q02 | P0 | Daemon | Boot in proxy mode, API answers | /ping pong, status=disconnected | PASS | config `listen:` override to 1778 works |
| Q03 | P0 | Connect | `up` with routes containing a **port-dimension rule** (`{network:udp, port:["9999"]}→direct`) + catch-all proxy | connected; port route accepted | PASS | Wire-contract JSON → Go compile → engine accepts; connected in ~7s |
| Q04 | P0 | Data path | `curl -x socks5h://` through tunnel | exit IP == node IP | PASS | 18.182.139.255 == JP 5372; real wire + remote DNS + TCP relay on new binary |
| Q05 | P0 | Disconnect | `down` | disconnected | PASS | immediate |
| Q06 | P0 | Rules | `up` with `{preset:"games"}→direct` route | games.krs fetched + engine connects | PASS | `~/Library/Caches/k2/rules/games.krs` (20 KB) pulled from CDN release v2026.08.04; SP3 bundle live end-to-end |
| Q07a | P0 | Error | `up` with malformed URL (no ECH) | clean sync error, no zombie state | PASS | 511 "ECH config required", state stays disconnected |
| Q07b | P0 | Error | `up` valid URL shape, blackhole IP (192.0.2.1) | error surfaces, never eternal "connected" | PASS | transport race → sync 511 "all candidates failed or timed out", state=disconnected. **Obsoletes 2026-02-26 CRITICAL T03** — connect now fails synchronously |
| Q08 | P1 | UI | Webapp UI regression | — | SKIP:no-runtime-change | branch webapp diff is type-only; vitest full suite green (test_build.sh 15/15) |
| Q09 | P1 | App Bypass | per-app routing | — | SKIP:no-runtime-change | no engine behavior change for existing configs; unit suites cover |
| Q10 | P0 | Mobile | iOS/Android regression | — | BLOCKED:Task-4 | real-device smoke required |
| Q11 | P0 | UDP | Full-cone NAT through a real tunnel (proxy mode, `scripts/check-nat-type.py`) | 3 distinct peers each reply from their own address; one shared public mapping | PASS | Oregon canary on SP1 server `d8619b93` + client `7682a659`: google/cloudflare/nextcloud all answered themselves, mapping `35.88.216.55:51488` for all three |
| Q12 | P0 | UDP | Same check against a **pre-SP1** server (JP 5372, `c911d237`) | must NOT pass — proves the check discriminates | PASS (fails as required) | All three requests misdelivered to google's STUN; cloudflare and nextcloud never received anything. This is the games-breaking bug SP1 fixes |
| Q13 | P0 | Regression | TCP + remote DNS through the fixed client | exit IP == node, HTTPS 200 | PASS | `https://www.google.com` 200 in 0.66s, exit 35.88.216.55 |
| Q14 | P0 | UDP | TUN-mode NAT type + real game + Telegram/KakaoTalk voice (#3051/#3345) | — | BLOCKED:Task-4 | needs attended TUN + phones. Proxy-mode Q11 already proves the wire/server halves |

## Session Notes

- Proxy mode is the safe regression vehicle on a shared dev machine: no TUN, no root, no route hijack, yet exercises the full wire/DNS/rule-engine path against production nodes.
- UDP full-cone itself is covered by SP1's e2e (multi-dest echo servers) and engine race suites; system-level TUN UDP deferred to Task 4 attended smoke.

---

# SP4 Release Gate — Hong Kong node, real game protocol (2026-08-10)

**Date:** 2026-08-10
**Platform:** macOS, client `k2 0.4.8 (7b8ec0a4)`, **proxy mode** (no TUN, no root)
**Node under test:** hk.lightnode.wm01 `38.54.23.249`, k2s self-reporting `version=0.4.8 commit=7b8ec0a4` — the same tree as the client, and the same tree proposed for release (k2 submodule `0627e92`, which contains both the full-cone fix `7682a65` and the dead-mux rebuild fix `183e09d`)
**Control node:** hk.aliyun.wm01 `8.210.31.61`, pre-SP1 `c911d237` (k2 `5885ce3`, `net.DialUDP`) — **not modified**, connected to only as an ordinary client
**Why HK:** lowest live-user count of the two HK nodes at test time (0–7 concurrent vs 17–42), so a restart disturbed the fewest people. No other node was touched.

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| H01 | P0 | Build | Node and client both self-report the release tree | `0.4.8 / 7b8ec0a4` on both ends | PASS | Gate anchored to the shipped artifact, not the image tag |
| H02 | P0 | Data path | SOCKS5 egress IP | == node IP | PASS | 38.54.23.249 |
| H03 | P0 | UDP | Full-cone NAT (`scripts/check-nat-type.py`) | 3 distinct peers each reply from their own address; one shared mapping | PASS | google/cloudflare/nextcloud all answered themselves; mapping `38.54.23.249:44671` for all three |
| H04 | P0 | Games | Real game protocol to 4 real game servers on one source port (`scripts/check-game-udp.py`) | every reply attributed to the server it was asked of; MOTD parses; loss/jitter playable | PASS | The Hive / NetherGames / CubeCraft / Galaxite, live player counts parsed; 0.0% loss on all four; jitter 9–37 ms |
| H05 | P0 | Games | Single tunnel session actually carries all four destinations | one session, all packets | PASS | client log: `quicUDPConn: closed sessionID=1 target=141.11.39.2:19132 txPackets=64 rxPackets=64 txDrops=0` — 64 packets to 4 servers on **one** session, zero drops |
| H06 | P0 | Games | Same test against the **pre-SP1** control node | must FAIL — proves the check discriminates | PASS (fails as required) | 3 of 4 replies came from The Hive, the session's first destination, carrying The Hive's MOTD and player count. NetherGames, CubeCraft and Galaxite never received a packet. This is the games-breaking bug, reproduced with a real game protocol |
| H07 | P1 | Latency | Tunnel latency vs direct | tunnel adds a plausible detour, not a bypass | PASS | direct 64–72 ms, tunnel 137–145 ms — consistent with a real HK detour |
| H08 | P0 | Mobile/TUN | TUN-mode NAT type, real game client, Telegram/KakaoTalk voice (#3051/#3345) | — | BLOCKED | needs attended TUN + phones |

## Two false results this session, both caught before they became conclusions

1. **A false MISDELIVERY.** The first version of `check-game-udp.py` matched replies to
   requests by arrival order. With four destinations whose RTTs spanned 60–330 ms on one
   socket, any reply that missed its timeout was consumed by the *next* exchange, and every
   later reply was off by one from then on — 81 of 100 "wrong peer" against a node that was
   delivering correctly. The tell was a median RTT of 8 ms through a Hong Kong round trip.
   Fixed by correlating on the seq the pong echoes back; never on ordering.
2. **A false PASS, and the more dangerous one.** The first game runs used `match: {}` as the
   catch-all. That is not the catch-all — `MatchConfig.All` is (`match: {all: true}`), and an
   empty match leaves `fallback = outboundDirect`. The game traffic went out **direct**, never
   entering the tunnel, and scored a clean PASS with 0% loss. Two independent tells: the node
   log had zero packets on port 19132, and the client log had `udpConns=0` with no
   `quicUDPConn` session. Every verdict here is now backed by a client-side session log
   showing the packets actually traversed the tunnel (H05).

Both are the same lesson in different clothes: a verification tool reports what it measures,
not what you meant to measure, and only a known-bad control (H06) tells the two apart.
