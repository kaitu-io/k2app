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

---

# 发布回归 UAT — v0.4.8..main（k2 5885ce3→ef6c0b2）macOS 进程级（proxy 模式，真实现网节点）

**Date:** 2026-08-14
**Build:** k2-uat = k2@ef6c0b2 `go build -tags nowebapp ./cmd/k2`（含 kuic e71de59f 子模块）
**Node:** 65.49.200.32（现网旧版 k2s：支持 QUIC echo、**不支持** TCP-WS echo）
**Method:** 独立 daemon :17771 + SOCKS :11080；pf anchor `com.apple/250-uatblock` 双向封 UDP/节点:443（须双向——服务端回包会被入向 pass 规则建 state 绕过出向 block）；封锁需 `pfctl -k` 杀既有 state

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| R01 | P0 | kuic 互通 | 新 kuic 客户端连现网旧节点（QUIC/Chrome-146 指纹/ECH/pin） | 握手成功、流量通 | PASS | race winner=quic-443 675-805ms；出口 IP=节点 IP；204 探测 0.49s |
| R02 | P0 | 兼容 | 旧订阅 URL 带 `hop=40000-40019` | 静默忽略、正常连接 | PASS | 生产 URL 原样连接成功，无告警 |
| R03 | P0 | TCP-WS 证书 | TCP-WS 腿 pin 校验（fc6b107 修复后）对现网节点 | 握手过、无 pin mismatch | PASS | uTLS Chrome 120_PQ + hostname SNI + ECH + metadata auth OK；dnsTransport=tcpws 持续承载 DNS；全程 0 处 code=403/pin mismatch |
| R04 | P0 | 记分板 | DIAG heartbeat 新字段 | activeChannel/scoreQuic/scoreTcpws 出现 | PASS | scoreQuic=1.00；QUIC echo 探测自适应 4→16s 节奏 |
| R05 | P0 | 会话中封锁 | pf 双向封 UDP → 自动恢复 | 切换 TCP-WS、流量续 | PASS | 空闲场景：echo strikes 3 次(3s 节奏)→re-race→tcpws 主传输替换，~8s；带流量场景 ~18s；切换后 204 探测 0.50s |
| R06 | P0 | 封锁下冷启动 | 全封 UDP 下 down/up | race 直接选 tcpws，无幻影事件 | PASS | winner=tcpws 2167ms（含 QUIC 800ms head-start），总连接 2.3s；无 phantom transport-switch（C1） |
| R07 | P0 | 旧节点 echo 兼容 | TCP-WS echo 不支持路径 | IsEchoUnsupported 良性 | PASS | `DIAG: echo-probe-unsupported transport=tcpws`；score 停初始 0.25、不记失败、无日志风暴 |
| R08 | P1 | 解封稳定性 | 解封后观察 | 无震荡/flap | PASS | 解封后 5min+ 零额外 race/switch；tcpws 上 dialOk=10 fail=0 |
| R09 | P1 | 重连回归 QUIC | 解封后 down/up | QUIC 重新胜出 | PASS | winner=quic-443 805ms |
| R10 | P1 | 规则包 | EnsureBundles 生产源在线下载 | 全 bundle 落地 | PASS | 20 个 .krs 含新 games/tm/kz/uz；k2-rules v2026.08.14 krs.tar.gz 有效 |
| R11 | P2 | 质量通路(丢包) | dummynet 20-30% UDP 丢包 → 质量降级路径 | scoreQuic 下降、m8 观察 | SKIP:canary-covers | 新版 macOS pf+dummynet 不可靠；m8(0.7 阈值震荡)本就是 canary DIAG 指定盯梢项 |
| R12 | P2 | 网络切换 Reset | Wi-Fi 切换清分数/棘轮 | Reset 清空 | SKIP:mobile-real-device | 桌面不折腾用户 Wi-Fi；engine race 测试已锁 Reset 语义；归真机清单 |

## 已知观察（非阻塞，与 review follow-up 对齐）

1. **DIAG `activeChannel` 与 `transport` 不一致**：tcpws 主传输拓扑下 heartbeat 显示 `transport=tcpws activeChannel=quic`——selector 按 C1 守卫在 tcpws-primary 拓扑惰性化，activeChannel 停默认值。功能正确、观测误导（对应 follow-up #3/#4 家族）。
2. **桌面实际恢复路径是 re-race 而非 selector 回滚**：engine echo strikes(3×3s)比 selector 的 3 连握手失败快，先触发 re-race 换主传输。selector 的 echo 门控回滚路径由单元测试+变异验证兜底（`TestProbe_RollbackRequiresEchoNotJustHandshake` 变异后确认变红）。
3. teardown 时 QUIC drain 期一条 `echo-probe-unsupported transport=quic-443`（context canceled 被归 unsupported）——一次性、无害，可留意。

## 判定

桌面进程级（UAT 阶梯第 1 层）达成：五场景 3 PASS + 2 SKIP（皆有结构性理由与替代覆盖）。TCP-WS echo 的正向路径（score 上升、review 周期、echo 验证回滚端到端）依赖新 k2s 服务端——归 canary 节点部署阶段。

## 真机 iOS smoke — 2026-08-14 23:27–00:15 (+07)，iPhone 15 (iPhone15,4)，commit b1c28668

设备日志取证：工单 617a041f 附带 k2.log/native.log/webapp.log（Center download_device_log 拉取）。

| ID | Pri | Test Case | Expected | Status | Evidence (UTC) |
|----|-----|-----------|----------|--------|----------------|
| M01 | P0 | 首连（race） | 3 候选 race，QUIC 胜出 | PASS | 16:27:56 race-start candidates=3 → winner=quic-443 1118ms → connected 1157ms |
| M02 | P0 | 飞行模式断网 | EngineError 101 network | PASS | 16:28:03 wire-error code=101 双腿 unreachable，分类正确 |
| M03 | P0 | 飞行模式恢复 | 自动恢复，无 engine 重启 | PASS | 34s 完整周期：16:28:37 quic→tcpws(quic-handshake-threshold) → 16:29:01 flap-damped 迟滞抑制 → 16:29:17 quic-recovered 回切。**记分板降级/抑制/回切全周期活体验证** |
| M04 | P0 | Wi-Fi→蜂窝 | 接口重绑、连接续 | PASS | en0(idx18) → 16:28:33 pdp_ip0(idx3) direct-bind 重绑；单 engine 会话贯穿；00:06:59 回 en0 |
| M05 | P1 | 后台省电（10min） | 备用通道零主动探测 | PASS | 全程 backup-probe 0 条（appext MobilePowerSaving=true 生效）；主通道 echo ~16s 均匀（预期，断连检测必需） |
| M06 | P1 | 稳定性 | 无 crash/jetsam/OOM | PASS | native.log 0 crash 标记；100 分钟单会话；全程 wire-error 仅 M02 那 1 条 |

杂音（与发布无关）：webapp 4× `402 membership expired`（测试账号会员过期，隧道列表拉取失败但既有连接不受影响）。

真机 iOS smoke（UAT 阶梯第 2 层）判定：M01–M06 全 PASS，其中 M03 是双通道记分板核心新行为在真机网络扰动下的端到端验证。R12（网络切换 Reset）由 M04 覆盖。剩余缺口：Android 真机（低风险——同 gomobile appext 路径）、canary k2s 服务端环、Windows 装机 smoke。

## Canary：SG 新 k2s × 新核心互通（TCP-WS echo 正向路径）— 2026-08-15 00:32–01:17 (+07)

部署：sg.aws.wm01 (13.213.183.161) + wm02 (18.136.83.32) 升级 `v0.4.8-9d7c6f95`（sidecar healthy、注册成功、44 端口映射）。客户端：k2-uat（k2@ef6c0b2，proxy 模式）连 wm01（隧道 id 6683，SNI 伪装 + ECH）。日志：scratchpad `uat2/k2-uat.log`。

| ID | Test Case | Expected | Status | Evidence (+07) |
|----|-----------|----------|--------|----------------|
| C01 | TCP-WS echo 正向：score 上升 | scoreTcpws 脱离 0.25 中性值 | PASS | 00:32 连接后 90s 内 0.25→1.00；re-up 会话 30s 到 1.00（信任阶梯 1s/2s/4s 起步） |
| C02 | review 周期 | score 随采样按 review 节奏更新 | PASS | ~60s 周期波动 0.63–1.00（真实 RTT 42–131ms 驱动） |
| C03 | 真实故障切换（非人为） | echo strike→re-race→tcpws 接管 | PASS | 01:11:32 echo-probe-fail×3(deadline)→01:11:38 transport-rerace(echo-consecutive-fail)→01:11:40 tcpws 胜出接管 primary，全程 6s，流量无中断 |
| C04 | UDP 全封下 tcpws primary 承载流量 | 代理流量 204 | PASS | pf 双向封 UDP+杀 state 后 `traffic-on-tcpws: 204 t=0.095s` |
| C05 | 解封后 QUIC 恢复 | race 回 QUIC，分数恢复 | PASS | re-up 01:16:14 race winner=quic-443 208ms；30s 心跳 scoreQuic=1.00 scoreTcpws=1.00 |

盯梢项活体样本：**m8 阈值震荡区证实**——健康链路 scoreTcpws 短暂 0.63（<0.7 阈值），canary 观察期重点。
新观察（入 follow-up）：**tcpws-primary 拓扑下 3.5min 内零 QUIC 恢复探测**（scoreQuic 冻结 0.25，无任何 probe 日志）——桌面回 QUIC 只有 re-race/重连一条路，与既有记录一致但更尖锐：死掉的 quic 通道不被后台探测。

判定：canary 环（阶梯第 3 层）首日证据全绿；TCP-WS echo 正向路径缺口关闭。⚠️ `:latest` 已指向新版，未 pin 车队将于北京时间 04:00 nightly auto-update 全量滚。

## 全车队 k2s 部署 v0.4.8-9d7c6f95 — 2026-08-15 01:35–01:52 (+07)

范围：24 台 active 节点（AU3/CA2/HK2/JP5/KR2/SG2/US8），SG 两台为此前 canary。方式：分 4 波并行（每波每区域≤1 台），`sed K2_VERSION` → `compose pull` → `up -d --remove-orphans`。升级前版本碎片：c911d237×16、b582ef6a×4、7b8ec0a4×2、d8619b93×1（印证"上次全量未执行"——根因=车队全 pin 旧 tag，nightly auto-update 只 pull pin 版）。

| 验证 | 结果 |
|------|------|
| 双容器镜像 tag=9d7c6f95 | 24/24 |
| sidecar healthy | 24/24 |
| Registration completed（升级后重注册） | 24/24（SG 两台早前已证） |
| 端口映射 | 24/24 齐全（bwh1/bwh4 宿主无全局 IPv6 故无 `[::]` 行，非回归） |
| 外部 443/TCP TLS 握手（本机直测） | 24/24 |
| 真实数据面 spot check | JP wm01 隧道 race quic 962ms、204 出口 35.79.73.82；SG wm01 桌面生产 VPN 持续在线 |

途中问题：ECR 匿名拉取限流（bwh1/bwh3 首次 pull toomanyrequests，重试即过，失败时旧容器未受影响）；首次串行脚本被超时杀于 AU wm04 `up -d` 中途，波次重跑修复。已知噪音：k2s stdout 的 "New version available: 0.4.1" 是陈旧版本检查提示，忽略；"server ready" 不在容器 stdout（wrapper 日志），可达性判据改用外部 TLS 握手。

判定：**全车队 24/24 部署完成并验证**。auto-update cron 在 root crontab（每日 04:00 +08），pin 策略下 nightly 恒拉 pin 版；下次发版需再跑批量 pin 更新。DIR-MIGRATION GUARD 可解除：全车队已在 /apps/k2s，零 legacy 目录。

---

# 0.4.8 发布验证编排 — 2026-08-18（按 v0.4.7..HEAD 变更点重新推导）

**背景**：0.4.8 于 2026-08-03 由 CI 构建并上传 S3，但**第二阶段 publish 从未执行**——桌面 `LATEST`/`cloudfront.latest.json` 仍是 0.4.7，Android `latest.json` 仍是 0.4.7，iOS 4.4.8 被 DEVELOPER_REJECTED 自撤。S3 上那份 0.4.8 产物是 8-03 的旧内容，**必须重新构建覆盖，不可直接切 latest**。

**本次范围（已决策）**：
- 品牌：**仅 kaitu**。Overleap 不纳入（S3 `overleap/` 树空、ASC 未上架 = 首发，另排）。`publish-web-ota` 恒双品牌，会在 `overleap/web/` 下生成 manifest——无消费方，无害。
- 节奏：**iOS 解耦**。桌面 + Android + Linux 先发，iOS 并行走真机 UAT + 重新提审，批准后自行上架。
- Web OTA：**全启用**（四端）。

**线上基线**：全平台 0.4.7（iOS 4.4.7 READY_FOR_SALE）。Web OTA 从未发布过（`kaitu/web/` 空）——本次是 bootstrap 首发。

## 变更全景（v0.4.7..HEAD = 376 app commits + 173 k2 commits，738 文件 +37661/−5072）

这不是补丁版本，是三条大功能线合流。按"是否已有验证证据"分层：

### 第 1 层 — 已被三层阶梯验证（wire/传输层，本次最大技术风险）

k2 传输层是本次改动最深的地方：QUIC 腿从 `apernet/quic-go` 迁到 `kaitu-io/kuic`（TLS 引擎换 `metacubex/utls`）、ClientHello 换 Chrome-146-over-QUIC 指纹、**移除 port-hopping 固定 443**、新增双通道质量记分板 + 迟滞切换 FSM、修复 TCP-WS 腿丢失证书校验（pin 形同虚设的可 MITM 洞）。

**证据链已闭合**（见本文件上方三段记录）：
- 桌面进程级 R01–R12（k2@ef6c0b2）：kuic 互通、旧 `hop=` URL 兼容、TCP-WS pin 校验、记分板字段、会话中封锁自动切换、封锁下冷启动、旧节点 echo 兼容、解封稳定性、规则包下载 —— 10 PASS + 2 SKIP（皆有结构性理由）
- iPhone 15 真机 M01–M06：race 首连、飞行模式 101 分类、**记分板降级/迟滞抑制/回切全周期活体验证**、Wi-Fi→蜂窝 direct-bind 重绑、后台省电零探测、100 分钟零 crash —— 全 PASS
- Canary 新 k2s × 新核心 C01–C05：TCP-WS echo 正向路径、review 周期、真实故障切换 6s、UDP 全封下承载、解封恢复 —— 全 PASS
- **全车队 24/24 部署 `v0.4.8-9d7c6f95`（2026-08-15），生产运行 3 天**；期间存量 0.4.7/0.4.3 客户端工单只有常规体验类，**无大面积断连** → 服务端对存量客户端向后兼容已被生产验证。

> 注意这些 UAT 覆盖的核心是 **`ef6c0b2`**，而 HEAD 是 **`6bc70b0`**——差 56 文件 / +4462 行，见第 2 层 C 段。

### 第 2 层 — 零验证证据，本次必须覆盖

| 面 | 规模 | 为什么危险 |
|---|---|---|
| **webapp**（阶段 D） | 176 文件 | 本次最大变更面。Router 顶级标签、国家排除过滤、端口/协议/游戏规则、双连接互斥、chunk-reload-guard、brands/ 重组——全是 0.4.7 用户升级后**直接看得见**的东西 |
| **桌面壳**（阶段 A） | 25 文件 | `kaitu-ui://` 自定义协议 + localStorage origin 迁移 + 移除 tauri-plugin-localhost（0.4.7 SIGABRT 根因）+ panic hook |
| **Web OTA**（阶段 B） | 四端全新 | 桌面 Rust / Linux Go / Android Kotlin / iOS Swift + CI + minisign + 回滚状态机。**首发即 bootstrap** |
| **移动端原生**（阶段 A/D） | K2Plugin +220 行 | `updateConfig`、`get/setUpdateChannel`、`routerRequest`、`getDefaultGateway`、minisign 验签；iOS `PacketTunnelProvider` + Info.plist + entitlements + vendored `K2RouteShim.h`。**唯一没有热修通道的一层** |
| **k2 `ef6c0b2→6bc70b0`**（阶段 C） | 56 文件 | provider 12（macOS lsof 进程归属重构、IPv6 归属修复）/ wire 9 / webui 7 / rule 7 / engine 5 / daemon 4 / core 4 / **server 3（ECH config_id gate）** |
| **认证与计量**（阶段 E） | api+mobile+webapp | tunnel token 全链路、per-device 流量计量 → 后台排行/超量告警邮件 |

### 已排除的风险（调查后确认，不必再测）

- **ECH config_id gate 不会踢掉合法客户端**：`server.go` 新增按 plaintext `config_id` 分流（自有→k2v5，外来→伪装站）。实测 HK 5458 节点持有 `{175,176,231}` 三份 ECH config，`LoadAllECHConfigs` **加载全部**（注释明写 supports rotation）进 `EncryptedClientHelloKeys`，而 Center 下发的 `ech=` 解出 `config_id=231` ∈ 该集合 → `ownsECHConfigID` 命中。JP/US 抽样同理。**升级节点不会因 config_id 漂移断连。**
- **origin 迁移不威胁登录态**：桌面 auth token 存 Rust `storage.json`（`_platform.storage`），localStorage 只承载偏好/缓存。迁移失败的兜底是"以空存储进入新 origin"，最坏丢语言/公告已读。
- **tunnel token 目前是软路径**：全车队 `K2_ENFORCE_AUTH=0`（k2s 与 sidecar 均是），节点不强制。存量 access token 继续可用 → tunnel token 出问题不会导致断连。**代价是这条链路在生产上从未被强制验证过**（阶段 E）。
- **旧 `hop=` 订阅 URL 不会失效**：R02 已 PASS，解析时静默忽略。

## 真机清单

| # | 设备 | 用途 | 关键性 |
|---|---|---|---|
| **D1** | macOS Intel + macOS **13.x**，**保持 0.4.7 不动直到 A01** | 0.4.7 SIGABRT 的唯一正对照环境（崩溃报告来自 Intel/13.7.8）+ 升级路径 | ★★★ **不可替代且一次性** |
| **D2** | macOS Apple Silicon | 主验证机（可反复重装/回滚） | ★★★ |
| **D3** | iPhone | NE/原生层 + 升级路径 + OTA + IAP | ★★★ |
| **D4** | Android 主力机 | VpnService + 原生层 + OTA + beta 通道 | ★★★ |
| **D5** | Android **停在 0.4.7 不升级** | `min_native` 闸门负向门 | ★★ **一次性** |
| **D6** | Linux x86_64 | 安装脚本 + webui 磁盘覆盖 | ★★ |
| **D7** | Windows 10/11（**按要求排最后**） | 装机 + 服务安装 + 签名 + appbypass 可见性 + origin 迁移 | ★★★ |
| D8 | iPhone 停在 4.4.7 | iOS 侧负向门 | 可选 |
| D9 | k2r 路由器实体 | 阶段 D 的 Router 段全链路 | 若无 → 该段整体 `SKIP:no-hardware`，**必须在发布说明里降级该功能的对外承诺** |

> **一次性资源警告**：D1 与 D5 一旦升级就无法复原为 0.4.7。请勿在编排开始前对它们执行任何更新（含系统自动更新提示）。

## 执行顺序原则

按"排除风险的杠杆率"排，而非按平台罗列：

1. **阶段 0 台面门禁** → 不消耗真机
2. **阶段 B 优先于功能面** → Web OTA 是**修复能力本身**。它通了，webapp（176 文件，最大变更面）的问题就从"发布事故"降级为"发布后热修"。先验能不能修，再验有没有坏。
3. **阶段 A 一次性设备** → D1/D5 只有一次机会
4. **阶段 C/D/E 功能面** → 可反复
5. **Windows 最后**（按要求）

---

## 发布阻断项 — 0.4.8 桌面对每个用户都是白屏（已修，`4579cb8a`）

**这是本次编排最重要的发现，也是唯一一个只有装真产物才能发现的问题。**

三条启动路径（`web_ota.rs` DiskUi / EmbeddedUi、`storage_migration.rs` 迁移后）
全部导航到 `ui_origin_url("index.html")`，而 webapp 的 `BrowserRouter` 路由表挂在
`/` 下且无 catch-all → `location="/index.html"` 匹配不到任何路由 → 渲染空树。

**它没有任何报错面**，这才是它能被打包发出去的原因：

- Rust 侧日志是一次干净启动，无异常
- `ui_boot_ok` 握手照常触发（它只证明 bundle 的 JS 跑过，**不证明渲染出东西**）
  → `.boot-pending` 被清除 → **web OTA 的坏包回滚机制也不会介入**
- bridge / store / 轮询全部继续工作（CloudAPI 60s、daemon status 15s），日志一片健康
- 唯一线索是 react-router 的一行 `No routes matched location "/index.html"`

**dev 模式测不出来**：web OTA 启动流被 `cfg!(debug_assertions)` 关掉，
`yarn tauri dev` 永远停在 Vite 的 `/`，根本不走 `kaitu-ui://`。**只有 release 构建白屏。**

**白屏冒烟门当时是绿的**，因为它 `goto('/')`——正是能用的那条路径。外壳真正使用的
`/index.html` 从没被测过。**门测了产品不用的路径，就证明不了产品。**

修复与三道防线（每道都做了变异验证，不是推理）：

| 防线 | 内容 | 变异验证 |
|------|------|----------|
| 根因 | `ui_boot_url()` 返回 origin 根，三处调用点改指它 | revert 后 Rust 测试变红 |
| 不变式 | Rust 测试钉住"boot URL 的 path 段必须为空"（不只是字面量相等） | 同上 |
| webapp | `<Route path="*" element={<Navigate to="/" replace />} />` | 移除后冒烟门 `/index.html` 报 `SMOKE FAIL: TimeoutError`，`/` 仍绿——精确复现生产故障 |
| 门 | `smoke-dist.mjs` 遍历外壳能启动的**每一种** URL 形态 | 同上 |

实测确认（真实安装的 release 产物）：`kaitu-ui://localhost/`、`rootChildren: 1`、
`rootHTMLLen: 87741`、commit `4579cb8a`、界面正常。修复前同一路径 `rootChildren: 0`、
emotion 的 `css`/`css-global` 长度均为 0。

> **对整个编排的含义**：在此之前所有"绿"的证据都只覆盖 dev 或本地构建，
> **不代表 release 产物的行为**。此后阶段 C/D/E 全部改为在真实安装的 release
> 构建上跑。

---

## 阶段 0 — 台面门禁（无真机）

| ID | 项目 | 判据 | Status |
|----|------|------|--------|
| G01 | push 未推 commit | origin/main == main | **DONE** — 已全部推送，当前 `2300a6d0` |
| G02 | `build-windows` CI 红（SimplySign 云会话过期） | ci.yml 全绿（OSV-Scanner 除外，见 `project_osv_accepted_residual_vulns`） | 反复项 — CI 侧曾于 `89898dad` 全绿（含 build-windows），随后 **同日再次掉线**（`1a3621f4` 红）。已再次自助重连并用 `windows-sign-preflight.sh` 真实私钥签名确认。**这条会一掉再掉：会话有有效期，凡要出 Windows 产物就先跑一次 preflight**（`--list-slots` 有 token ≠ 能签，见下方记录） |
| G03 | web OTA 地板锚点 + 契约门 + iOS channel | 见下方 G03 记录 | **DONE** `ec25dfcf` |
| G04 | 契约门四道 | bridge-contract golden、api-contract（`-count=1`）、brand-purity、白屏冒烟门全绿 | **DONE** — 四道全绿。白屏冒烟门已**扩为遍历所有启动 URL 形态**（原门只测 `/`，漏掉了外壳实际使用的 `/index.html`，见上方阻断项） |
| G05 | 全量测试 | webapp vitest / cargo test / go test / `scripts/test_build.sh` | **api 已解除阻塞** 2026-08-19 — Docker 起来后 `dev-mariadb` 绑 `127.0.0.1:3306`（更精确者胜），`go test -v ./...` = **1211 PASS / 0 FAIL / 1 SKIP**，且 `skipIfNoConfig` 触发 **0 次**（判据是这个，不是耗时）。唯一 SKIP 是 `TestBrandIsolationMatrix/01_SameEmail_DualBrand…` 子测试自带。webapp 117 files/1337 passed、desktop cargo 180、mcp ok、manifest 门 11/11；`test_build.sh` **已跑完 14/15**（2026-08-19）——唯一的红是**门本身陈旧**而非产物：它把 iOS `MARKETING_VERSION` 与 package.json 逐字比，而 `1a3621f4` 后营销版本是重映射的（`4.4.8` 才对）。已在 `fix/test-build-ios-version-gate` 修正，见下方记录 |
| G06 | k2 submodule 指针 | `6bc70b0` 且 `LinuxBridgeVersion=2` | **DONE** — 已确认 |
| G07 | 版本对齐 | package.json / Cargo.toml / build.gradle / `k2AppVersion` 皆 0.4.8；iOS build 号 **4408990** | **DONE** `1a3621f4` — `check-versions` 扩为四方对齐并已在 CI 通过。iOS 编号方案重设计（`89898dad`）：`4000000 + MINOR*1e5 + PATCH*1e3 + SLOT*10 + REV`，0.4.8 正式版 = **4408990**，高于 ASC 上已烧掉的 440899 / 440900 |
| G08 | 重打 `v0.4.8` tag | origin 上现指向 `757467a5`（8-02 旧内容），需删除重打到当前 main | **等授权** |

### G03 执行记录（commit `ec25dfcf`）

`webapp-support-floor.json` 原为 `{"desktop":"0.4.9","linux":"0.4.9"}`。链路：floor → `web-ota-manifest.mjs` 写 `min_desktop:"0.4.9"` → 桌面 `web_ota.rs::evaluate_manifest()` → `meets_min_base(Some("0.4.9"),"0.4.8")` → **`Gate::Skip`**。即 0.4.8 桌面与 Linux 会被自己的闸门**永久静默挡在 web OTA 之外**。已改 0.4.8（修正锚点，非砍支持）。

另处理两项同链路问题：

1. **补齐缺失的契约门（结构性）**：既有门只有 `floor.bridge ≤ BRIDGE_API_VERSION` 一条 bridge 维度不变式，版本维度全靠人眼——所以 `0.4.9` 溜进来时全绿。补上同构的 `floor.{native,desktop,linux} ≤ package.json version`，落在提交期（`bridge-contract.test.ts`）与发布期（`buildManifest`）两处，两者**导入同一个 `compareBase`**。
2. **iOS update channel 是死能力**：`capacitor-k2.ts` 用 `getPlatform()==='android'` 硬门，iOS 上 `setChannel` 恒 `undefined`（尽管 Swift 侧早已实现）；反向缺陷是 Android 探测失败后仍暴露 `setChannel`。已改能力探测。**iOS 因此可直接跑 beta-first UAT。**

变异验证 4 次均确认目标测试真的变红。`capacitor-k2.test.ts` 此前对 channel **零覆盖**（mock 里连方法都没有），已补 3 用例。

### 已知发布链路陷阱

- 重打 `v0.4.8` tag 会同时触发 `release-desktop` + `build-mobile` + 尾 job `publish-web-ota`（对非 `-beta` 的 `v*` 恒发 **stable** web manifest）。首发 bundle = 同 commit 构建 = 与内嵌 UI 一致，兼容天然成立。
- S3 `kaitu/desktop/0.4.8/` 已有 8-03 旧产物，重发会覆盖同名文件——老 `.sig` 与新包不匹配，**产物与 latest.json 必须同批切换**，中间态不可对外可见。
- 发布是两阶段：CI 只上传产物，`publish-desktop.sh` / `publish-mobile.sh` 才切 latest。这正是 0.4.8 卡住的原因。

---

## 阶段 B — Web OTA 活体链路（**最先跑**，它是本次的热修能力本身）

### 隔离机制：为什么不能用 beta 频道验证（2026-08-18 调查）

| 事实 | 依据 |
|------|------|
| **桌面在野没有受众** | `v0.4.7` tag 的 `desktop/src-tauri/src/` 只有 `channel.rs`，**没有 `web_ota.rs`**（`3dca6c5c` 才引入）。现存桌面客户端不知道该端点存在 |
| **移动端在野已经在轮询** | `v0.4.7` 的 `K2PluginUtils.webManifestEndpoints()` 存在于 Android 与 iOS 两端 |
| **且是静默自动应用** | `performAutoUpdateCheck()` 下载 → sha256 → 解压 → 直接换掉 webapp，**不提示用户** |
| **0.4.7 移动端无签名门** | grep 确认两端均**无 minisign、无 min_bridge**。唯一完整性凭据是 manifest 里的 sha256，而它来自同一个 manifest |
| **唯一拦阻是 `min_native`** | 跑在**老客户端**里，发布时改不了、也回滚不了 |
| **两个频道当前都是空的** | `kaitu/web/latest.json` 与 `kaitu/web/beta/latest.json` 均 403（S3 无 ListBucket 时把 404 伪装成 403）。同主机的 `kaitu/desktop/d0.latest.json`、`kaitu/android/latest.json` 均 200 → 是"没发布过"而非"主机不通" |

`min_native` 门的正确性已逐条核验：Android `versionName "0.4.7"` → `[0,4,7] < [0,4,8]` → skip ✓。
iOS 曾是最可疑的一环——营销版本被重映射为 `4.4.7`，若比较用它则 `[4,4,7] >= [0,4,8]`
**门会反向失效**。实际读的是 `K2AppVersion`（构建期由 `K2_APP_VERSION` 注入真实语义版本），
ASC 显示 **4.4.7 READY_FOR_SALE** ⇒ 该次构建 `$VERSION` 非空 ⇒ 键有值、不走回退 ✓。
该回退路径已在 `1a3621f4` 结构性移除（改读编译期常量 `k2AppVersion`）。

**结论**：隔离必须来自**路径**，不能来自频道。`publish-web-ota.yml` 已加
`namespace` 输入（`2300a6d0`）：发布到 `{ns}/{brand}/web/`，空值 = 生产布局逐字节不变。
配合桌面 `K2_WEB_OTA_BASE=https://d0.all7.cc/uat/kaitu`（`ota_sources_for` 在有 override
时只返回那一个源，且**不放宽** sig / min_bridge / min_desktop 任何一道门）即可跑完整链路。

**免费的隔离证明**：UAT 跑完后 `kaitu/web/latest.json` 必须**仍然是 403**。
→ 实测通过（2026-08-19）：`kaitu/web/latest.json`、`kaitu/web/beta/latest.json`、
`overleap/web/latest.json` 三条全部仍为 403，而 `kaitu/web/uat/web/latest.json` 为 200。

### 发布阻断项候选：CI 的 S3 身份没有 overleap 写权限

UAT 发布暴露的，不是本仓库代码的问题：

```
AccessDenied … user/k2app-ci-s3 is not authorized to perform s3:PutObject
on "arn:aws:s3:::d0.all7.cc/overleap/web/uat/web/…"
```

同一次 run 里 **kaitu 四个键全部上传成功**，overleap 第一个键就被拒。只读探测
`overleap/{desktop,android,web}/…` 全部 403 —— overleap 在这个桶里**从未被写过
任何东西**，IAM 很可能根本没有 overleap 授权。

**对发布的影响**：`publish-web-ota.yml` 无条件遍历 `for BRAND in kaitu overleap`，
且 kaitu 排在前面。所以真正的 stable 发布会**先把 kaitu 生产路径写成功，再在
overleap 上失败** → run 变红、`Invalidate CloudFront` 被 skip。kaitu 用户拿到的是
已发布但 CDN 未失效的状态（新路径无缓存，实际影响小），而 overleap 什么也没有。

**已按 ① 处理并闭合（2026-08-19）**，但缺口是**两道而非一道**：

| 门 | 位置 | 缺了会怎样 |
|----|------|-----------|
| 写 | IAM 用户 `k2app-ci-s3` 内联策略 `k2app-desktop-release` / Sid `S3Access` | `AccessDenied … s3:PutObject`，CI 步骤红 |
| 读 | **桶策略** `PublicReadGetObject`（`d0.all7.cc`） | 对象写得进去，公开 GET **403**；`head-object` 却看得到对象——最容易误判成"没发上去" |

两处原本都只有 `kaitu/*`。各加一行 `arn:aws:s3:::d0.all7.cc/overleap/*`（与 kaitu
同形；桶策略跨项目共享，只加一条 Resource，meety/anc、waymaker、meet 未动）。
验证不是回读策略而是让 CI 真跑一次：8 个键全部上传成功，两个品牌 UAT 路径均 200，
生产四条路径仍 403。

> **403 有两种含义**：桶未授予 ListBucket 时，不存在的键也返回 403 而非 404。所以
> 生产路径的 403 是"还没发布"，overleap UAT 当初的 403 是"没有读权限"——两者靠
> `aws s3api head-object`（带凭证）区分。

**仍待决策**：授权解决"能不能"，不解决"这次要不要"。`publish-web-ota.yml` 仍无条件
遍历两个品牌，所以真正的 stable 发布会把 **overleap 也推上线**，与"0.4.8 kaitu only"
的既定范围冲突。选项 ②（给 workflow 加 brand 输入）仍然需要，且**不能做成"失败就
跳过"**——静默跳过一个品牌正是这条流水线最不该有的行为。

manifest 版本形如 `0.4.8.<2026-01-01 起的秒数>`，桌面 `is_newer_version` 会给短的补 0，
故 `0.4.8.2xxxxxxx > 0.4.8` 成立——UAT 能真正触发 apply，不会退化成"无更新"的假绿。

| ID | 设备 | 用例 | 期望 | Status |
|----|------|------|------|--------|
| B01 | — | 首发 manifest 推导 | `min_native/min_desktop/min_linux = 0.4.8`、`min_bridge = 1`、版本号为 `0.4.8.<epoch>` | **PASS** 2026-08-19 — UAT manifest 实测四项全对，版本 `0.4.8.19892011` |
| B02 | D2 | 桌面 OTA 全链路 | 拉取→sha256+minisign 验签→下载→原子换盘→重启生效（bundle 带肉眼可辨标记） | **PASS** 2026-08-19（macOS，真 CDN）— `downloading UI 0.4.8.19892011 from https://d0.all7.cc/kaitu/web/uat/web/…` → `applied UI 0.4.8.19892011 (was 0.4.8), effective next launch` → 重启 `serving UI 0.4.8.19892011 from disk`。**判别式不用人造标记**：entry script 从内嵌的 `main-BnsKHI3B.js` 变为 OTA 的 `main-CHBucI3N.js`；`href=kaitu-ui://localhost/`、`rootChildren=1`、`rootHTMLLen=87741` —— 白屏修复在**当初炸掉的 DiskUi 路径**上成立 |
| B03 | D2 | 桌面坏包回滚 | 白屏 bundle → `.boot-pending` 残留 → 移入 `quarantine/` + 回退 previous/内嵌 | **PASS** 2026-08-19 — 用真实坏包（入口 JS 前置 `throw`）而非模拟。启动1：磁盘 UI 起、JS 抛错、`ui_boot_ok` 不触发、`.boot-pending` 保留、poller 正确跳过该 tick（F1）。启动2：`previous UI boot unconfirmed — rolled back: RolledBackToEmbedded`，`current/` → `quarantine/0.4.8.19892011-20260819054448`，`quarantined-version.txt` 记版本，内嵌 UI 恢复正常渲染。再带同一 manifest 启动：`no update: version quarantined`（F2），不会装回来 |
| B04 | D3 / D4 | 移动端 OTA | minisign 验签通过，冷启动生效 | **部分** 2026-08-20 — web lane 的**执行**已在 Android 真机闭环（降版本探针，见下方专段）；剩下的 下载→sha256→minisign→落盘这一段仍 TODO，需要一个可达的 web manifest。该段与用户手动触发的 web 更新**共用同一个 `verifyWebZip` 核心** |
| B05 | **D5**（/D8） | `min_native` 闸门负向门 | 0.4.7 存量机正确**静默跳过**，不下载不报错 | **INVALID:用例前提不成立** 2026-08-19 — 存量机根本走不到 web 分支，`min_native` 从未被求值。真机实测见下方专段。这个用例若照原样执行会给出**假阳性**（「没下载、没报错」看起来像门生效，实际是 native 分支短路） |
| B06 | D6 | Linux 磁盘覆盖 | 页面刷新即新 UI；`?ui=embedded` 逃生口回内嵌 | TODO |
| B07 | 全平台 | 篡改测试 | 改 hash / 剥离 sig / 换签名密钥 → **全部拒绝应用** | **PASS（桌面）** 2026-08-19 — 本地 HTTP 服务器 + 真实 release 产物三发：① 合法 hash+伪造 sig → 下载后 `sig decode: Invalid encoding in minisign data`；② `min_desktop=0.9.9` → `no update: min_desktop not satisfied`，**HTTP 访问日志只有 latest.json、无 zip 请求**（门在下载前短路）；③ hash 全 0 → `sha256 mismatch`（早于 minisign）。移动端待真机 |
| B08 | 全平台 | 核按钮 | 清空 `latest.json` → 停止更新且不崩 | **PASS（桌面）** 2026-08-19 — 生产两个源当前天然就是 403（web OTA 从未发布），app 打两条 WARN 后继续正常运行，无崩溃、无降级 |
| B09 | D3 / D4 | beta 通道切换（`ec25dfcf` 后两端均具备） | `setChannel('beta')` 生效，端点切 `beta/` 前缀；0.4.7 存量机降级 stable-only 且不显示开关 | TODO |

---

## 阶段 A — 壳层与升级路径（**消耗一次性设备，排第二**）

| ID | 设备 | 用例 | 期望 | Status |
|----|------|------|------|--------|
| **A01** | **D1** | 该机保持 0.4.7 → 复现 SIGABRT → 装 0.4.8 启动 | 0.4.7 崩 / **0.4.8 不崩**（唯一正对照） | TODO |
| A02 | D1 → D2 / D7 | 从 0.4.7 覆盖升级 | `kaitu-ui://` origin 迁移后**登录态不丢**、语言/日志级别/公告已读保留 | **PASS**（macOS，2026-08-18；**Android 0.4.6→0.4.8 覆盖升级 2026-08-19 亦 PASS**——签名指纹一致故 `adb install -r` 2 秒完成、数据保留；会员状态/到期日 2034-12-28/5 台设备/简体中文逐项一致，节点列表能拉到数据即证明 token 有效。Android 无 origin 迁移问题：`capacitor.config.ts` 在 v0.4.6 与 HEAD 均未设 `androidScheme`，origin 不变；那条 `kaitu-ui://` 迁移是 Tauri 桌面独有）— 16/16 键迁移到新 origin，6 项关键偏好逐字节一致；鉴权端点 200，登录态完好。桌面认证本就存在 Rust `storage.json`（与 origin 无关），迁移只搬偏好与缓存 |
| A03 | D2 | panic hook 落盘 | 任意线程 panic 写入 `desktop.log`（早于 tauri-plugin-log 初始化） | **PASS** 2026-08-21 — cargo test `panic_hook_appends_panic_to_log_file` + `panic_hook_creates_missing_log_dir` 双绿（真触发 panic「boom」并断言落盘 + 缺目录自建）；hook 装在 main() 首句覆盖 plugin setup panic；活体 `~/Library/Logs/kaitu/desktop.log` 在位（0.4.8 启动 panic 根因已修故 0 条，无自然 panic 可观测） |
| A04 | D2 / D7 | single-instance | 第二实例 exit 0（~200ms），首实例窗口置前 | **PASS**（macOS，2026-08-18）— 第二实例 474ms exit 0，首实例收到唤起回调并存活 |
| A05 | D2 | Kaitu × Overleap 桌面并存 | 两品牌不抢 `io_kaitu_desktop_si.sock`（本地构建验；Overleap 不发布） | **PASS** 2026-08-21 — 文件系统实证：两 sock 并存且名字不同 `/tmp/io_kaitu_desktop_si.sock` + `/tmp/io_overleap_desktop_si.sock`；bundle id 各为 `io.kaitu.desktop` / `io.overleap.desktop`，single-instance sock 由 identifier 派生 → 不同路径 → 永不争抢。Kaitu 现运行持锁，Overleap sock 为前次运行遗留，共存 |
| A06 | D6 | `curl -fsSL https://kaitu.io/i/k2 \| sudo bash` | 安装成功、webui 可达、tarball sha256 校验通过 | TODO |
| A07 | D3 / D4 | 装机 + bridge v2 | `checkReady` 返回 `bridgeVersion=2` | **PASS**（桌面侧，2026-08-18）— `_platform` 面完整，`updater.setChannel` 是函数（`ec25dfcf` 的能力探测在桌面同样生效）。移动端待真机 |
| A08 | 全平台 | P0 连接 / 断开 / 错误显示 | 连得上、断得干净、错误走 i18n 不露原始串 | **PASS（连/断）+ FAIL:stale-poll-race**（macOS，2026-08-19；**Android 2026-08-19 连/断 PASS**，见下方 Android 段）— 连接与断开的后端行为都干净；但断开时若撞上进行中的状态轮询，UI 会被陈旧响应打回「已连接」。详见下方专段 |
| A09 | D3 | iOS NE 变更面 | `PacketTunnelProvider` + Info.plist + entitlements 改动后**隧道正常起停**；extension 有显式 `CURRENT_PROJECT_VERSION`/`MARKETING_VERSION` | TODO |

---

## 阶段 D — 功能面回归（0.4.7 用户升级后**直接看得见**的新东西）

> 本段是按变更点新增的，此前编排完全没有覆盖。

| ID | 设备 | 用例 | 对应变更 | Status |
|----|------|------|----------|--------|
| D01 | D2 / D4 | **Router 顶级标签**：发现→绑定→设备列表→改名/改密→解绑 | `0fe3e92c` 等 k2r 全套 | **部分 PASS**（macOS，2026-08-19）— 无路由器时 Router tab 确实不渲染 ✓；直接导航 `/router` 落到空 Outlet、**不白屏不崩、底部导航保留**（`rootChildren` 仍为 1）✓。绑定/改名/解绑全链路仍需 D9 |
| D02 | D9 | Router 控制密钥鉴权 + anchor 探测（`10.17.79.1`） | `55cf1113` / `91683414` | **BLOCKED:no-k2r-this-cycle** 2026-08-21 — 用户决定本轮暂不测路由器。设备够不到 k2r LAN（CU_601/10.17.79.x 独立网，WiFi 密码未提供），探测记录见下方专段 |
| D03 | D2 / D4 | **双连接互斥**：app 与路由器同时连 → 排他对话框 + 接管横幅 | `d3897578` / `773c8880` | **BLOCKED:no-k2r-this-cycle** 2026-08-21 — 需 `isRouterTakeover()` 为真（真实 k2r 在本机转发路径上）。用户决定本轮暂不测路由器 |
| D04 | D9 | 企业多槽表单（槽位列表、告警徽章、默认落地页） | `9ce69c02` | **BLOCKED:no-k2r-this-cycle** 2026-08-21 — 企业形态需 k2r `status.slots`。用户决定本轮暂不测路由器 |
| D05 | D2 / D4 | **国家排除过滤**：漏斗图标→对话框→排除 chip→自动选择生效 | `9a8a21f0` / `23b2f60f` | **PASS**（macOS，2026-08-19）— `auto-country-filter-btn` → 对话框列出 7 国 24 节点 → 勾选美国后徽章 `0`→`1` → 全选后 `7`；服务器面板出现 chip「自动选择 · 已排除 7 个国家/地区」，且每个节点条目标注「自动选择已排除 + 国家名」 |
| D06 | D2 | 全部国家被排除 → 错误码 **573** 文案正确（不是"未知错误"） | `2e48ec15` | **FAIL→PASS（已修，真机验证）** — macOS/Android 双平台确认失败形态后修复并合入。Android 真机同一操作路径前后对照：修复前折叠态「⚠ 未知错误」，修复后「**可用节点已全部被排除，请调整国家/地区过滤**」（读自 accessibility 树，非 OCR）。根因比症状大得多，详见下方专段 |
| D07 | D2 | **端口/协议/游戏规则**：MatchConfig 的 port/protocol/games 预设实际生效 | `b2d36efe` / `0be1d77` | **PASS（单测）** 2026-08-21 — 类型对齐改动，`client-config.test.ts` 4 tests 绿。类型层是恰当验证层级，运行时无独立可观测面 |
| D08 | D3 | **Apple IAP 建单**：沙盒购买 → 建单正确、首单判定不恒真、分销返现入账 | `07948c1d` / `3caabab6` / `fd179e9b` | **BLOCKED:sandbox-sub-exhausted** — 手上的沙盒订阅已过期且交易全在过去，验不出建单/返现。需换全新沙盒 Apple ID 做首购，见下方专段 |
| D09 | D3 | IAP 沙盒交易**不建订单**；会员过期续费不报"失败无信息"（对应工单 3537） | `fd179e9b` | **PASS（不建单）+ 发现独立 bug（已修）**（真机 iPhone，2026-08-19）— 不建订单符合设计；但"有效期没加上"暴露出 `applyRenewalCredit` 缺 from-now 规则，已修并合入 main。详见下方专段 |
| D10 | D2 / D4 | 绑定/修改邮箱时被占用 → **409001** 专用文案（不再显示"参数错误"） | `b5de67ff` | **PASS** 2026-08-21 — 两半独立验：① 服务端 `ErrorEmailAlreadyInUse=409001`，handler 测试双路径绿（占用→409001、自己的邮箱→非 409001），断言锚在发信前那道门；② 前端 `errorCatalog` 409001→`auth:updateEmail.emailAlreadyInUse`，7 语种文案齐（说明原因+行动指引，非「参数错误」）。矩阵原写「注册时」实为绑定/改邮箱同码路径 |
| D11 | D2 | 窄屏 Account 页邮箱行不溢出、不压住按钮 | `1fb5dbc8` | **PASS** 2026-08-21 — 活体 0.4.8 桌面 vw=403 实测：修复的三个机制均在 DOM 上确认——`overflowWrap:anywhere`、掩码正好 3 星（旧代码对 6 字符用户名产 4 星，`y***4@gmail.com` 即新码在跑）、次要按钮 `position:relative` 已从绝对定位移入正常流；邮箱 right=196 < 按钮 left=312 不重叠，行不溢出视口 |
| D12 | D2 | **chunk-reload-guard**：陈旧 chunk 场景自动恢复，不白屏 | `f5a2cea1` | **PASS（单测+活体旁证）** 2026-08-21 — `chunk-reload-guard.test.ts` 5 + `tauri-k2.boot-ok.test.ts` 4 全绿（含「只重载一次」「重载后仍失败则放弃」两道守卫）；活体 0.4.8 桌面正常冷启动即 ui_boot_ok 延后到首帧后仍成功的旁证 |
| D13 | D7 | **appbypass 应用列表可见性**（Windows 三连修） | `717552be` F2/F3/F4 | TODO |
| D14 | D2 / D4 | i18n 七语种关键页面无缺键（Router/国家过滤/573/409001 均为新增键） | 多处 | **部分 PASS + 1 FAIL**（静态，2026-08-19）— 七语种 1390 键集合完全一致（0 缺 0 多）；但源码侧 647 个静态 `t('ns:key')` 引用比对出 10 个键在七语种全不存在，其中 **`RouterDevices.tsx` 两处 `t('common:cancel')` 无默认值兜底 → 取消按钮渲染出字面量 `common:cancel`**（正确键 `common:common.cancel` 存在）。详见下方专段 |

---

### D06 根因：VPN 连接错误有两套映射，连接失败走的是缺 40 个码的那套

**这是结构性的，D06 只是它的一个样本。**

链路（每一环都实测/读码确认，非推理）：

1. `connection.store.ts` **正确**检出全排除并派发
   `BACKEND_ERROR` + `{code:573, message:'All auto-pick candidates excluded by country filter'}`
   —— console 实证：`[Connection] connect: Auto mode but no tunnel available (all candidates excluded by country filter), aborting`
2. `vpn-machine.store.ts` **正确**落 error（`nextState==='idle'` 会自动清 error，但 `payload wins` 覆盖回来）
3. **展开态**（默认）`ConnectionButton` 对 error 的唯一反应是 `t('common:status.error')` = **「连接失败」**，本就拿不到具体文案
4. **折叠态** `InlineErrorBar` 走 `t('common:' + getErrorI18nKey(573))`，而
   `vpn-types.ts` 的 `getErrorI18nKey` **errorMap 里没有 573** → `'errors.unknown'` = **「未知错误」**
5. error 在 ≤15s 后被下一次 status 轮询静默清除（`BACKEND_DISCONNECTED` → `idle` → `error=null`，payload 无 error 故不覆盖）

> 观测教训：第一次取样是在轮询清除之后，看到的是「未连接」，据此差点误判成"错误完全不显示"。
> 改为点击后 150/400/900/1800ms 连续取样才看到「连接失败」。**错误态有寿命，取样必须紧跟动作。**

**规模**（脚本统计，非估计）：

| 映射 | 覆盖 |
|---|---|
| `ERROR_CODES` 定义 | 70 个码 |
| `errorCode.ts` `getErrorMessage()` | 69 个，文案精确 |
| **`vpn-types.ts` `getErrorI18nKey()`**（VPN 连接失败走这条） | **9 个** |

引擎类错误码（100–599）共 47 个，`getErrorI18nKey` 未覆盖 **40 个** → 全部显示「未知错误」，
而其中 **38 个在 `getErrorMessage` 里早有精确文案**。文案不缺，是这条路径拿不到。

**是回归吗？不是。** `git show v0.4.7:webapp/src/services/vpn-types.ts` 的 `getErrorI18nKey`
与 HEAD **一字不差**（同样 9 个码）。但 **573 是 0.4.8 新增的**（v0.4.7 只有 572），
所以这是**新功能的验收未达标**，不是升级变差。

**发布影响**：不构成阻断（体验不比 0.4.7 差），但国家排除过滤是本次主打功能，
用户全排除后看到「未知错误」且无从下手 → 直接转工单。修复成本极低（errorMap 加行），
且在 webapp 层 → **Web OTA 可热修**。

**修法警告**：这是"同一枚举被 N 个手写谓词列举"的形状（见
`feedback_same_shape_bug_means_structural_fix`）。逐个补码是第三次补洞；
正确做法是让两套映射共用一个事实源，并加覆盖守卫（新增错误码若两处都没有 → 测试红）。

### D14 附带发现：`RouterDevices.tsx` 的取消按钮会渲染出键名

`t('common:cancel')` × 2 处，**正确键是 `common:common.cancel`（已确认存在）**，且**未传默认值**。
`i18n.ts` 没有配 `parseMissingKeyHandler` / `saveMissing` / `returnNull`，i18next 默认行为即**回显 key**
→ 两个对话框的取消按钮显示字面量 `common:cancel`。落在本次新增的 Router 面（D01）。
待 D9 到位时活体复核。

另 9 个缺键有默认值兜底、不崩，但语言错位：`common:common.next`→「下一步」、
`invite:invite.qrCodeGenerationFailed`→「二维码生成失败」**给英语用户看中文**；
4 个 `common:errors.vpn.*` 给中文用户看英文。

> 本检查的盲区（如实记）：27 处动态键（模板串/变量）抓不到；
> "目标语言值与中文源逐字相同"这条判别式基本失效——ja 的「保存」「成功」、
> zh-HK/zh-TW 的繁简同形字都是合法同值，区分不出真未翻译。

### 台面门修正：`test_build.sh` 的 iOS 判据（`f0f67227`）

跑 `test_build.sh` 得 14/15，唯一的红是门陈旧：它把 `MARKETING_VERSION` 与 package.json
逐字比，而 `1a3621f4` 后 iOS 营销版本是重映射的（`4.4.8` 才对）。CI 的 `check-versions`
早已改成四方对齐，**两道门对同一件事判据矛盾**。

修的时候发现更深的问题：**这一整节跑在 `make pre-build` 之后**，而 pre-build 会调
`sync-version` 把这些文件写成 `build-mobile-ios.sh` 说的值。所以只要"向生产公式要期望值"，
公式自己坏掉时两边一起漂移，**门永远绿**。故补两条不调用该脚本的独立判别式
（重映射不变式：major 固定 4、minor/patch 跟随 package.json；build 号形状 `4NNNNNN`）。

变异验证三次，每次只打中一道防线，互不重叠：

| 变异 | 独立判别式 | 比对门 |
|------|-----------|--------|
| 重映射 `4.` → `5.` | **FAIL** | 绿（拿到 5.4.8 仍报 match） |
| build 号返回 `oops` | **FAIL** | — |
| `sync-version` 的 pbxproj sed 失配 + 仓库留旧值 4.4.7 | 绿 | **FAIL** |

基线 9/9（原 5 项）。

> 观测更正：先前记的 "有失败却 exit 0" 是我自己管道里 `tail` 的退出码，脚本 `exit 1` 逻辑本身正确。

### ⚠️ 本地 `build-macos-test` 产物名叫 universal，内容是 arm64-only

`make build-macos-test` = `--single-arch --skip-notarization --features=mcp-bridge`，
产出 `release/0.4.8/Kaitu_0.4.8_universal.pkg`，但 `pkgutil --expand-full` + `lipo -info`
证明内部是 **`Non-fat file: arm64`**。命名没跟着 `--single-arch` 走。

**对本编排的实际杀伤**：**D1 是 macOS Intel，且 A01 是一次性不可复原的对照实验**。
拿这个包去装 D1 会直接毁掉该实验。**给 D1 的必须是 CI 产出的包，不是本地 build-macos-test 产物。**

### A08 附带发现：陈旧轮询响应会把「已断开」打回「已连接」

真机时间线（console，非推理）：

```
07:44:39.903  run: action=status                          ← 15s 轮询发出（那一刻确实还连着）
07:44:39.915  handleToggleConnection: → disconnect        ← 12ms 后用户点断开
07:44:39.978  SSE: state=disconnected
07:44:39.981  disconnecting + BACKEND_DISCONNECTED → idle ← SSE 正确，状态机到达 idle
07:44:40.018  run: action=status → code=0                 ← 39.903 那个请求现在才回来
07:44:40.019  statusToEvent: connected → BACKEND_CONNECTED
07:44:40.019  idle + BACKEND_CONNECTED → connected        ← 陈旧响应覆盖正确状态
```

后端此刻已 `state=disconnected`、出口 IP 已回本地，**UI 却显示「已连接」+ CheckIcon +
「已连接中，切换服务器前请先断开」**。用户会以为自己受保护，实际流量在裸奔。

**恢复要 2 分 07 秒**（`07:46:47` 才 `connected + BACKEND_DISCONNECTED → idle`）——
connected 态的轮询间隔远长于 disconnected 态，把 15s 的窗口放大成分钟级。

触发窗口 ≈ 轮询往返耗时（本次 115ms）/ 轮询周期，概率不高但后果严重，且**断开正是
用户最需要状态可信的时刻**。

**根因**：轮询响应与 SSE 事件竞争同一个状态机，而轮询响应**不带任何时序标记**，
后到者赢。讽刺的是 `connection.store` 已有 epoch 机制（日志可见
`connect: epoch=0→1`、`disconnect: bumping epoch`），只是没用在轮询响应上。

**是回归吗？不是。** `git diff v0.4.7 HEAD -- webapp/src/stores/vpn-machine.store.ts`
**零改动**，转移表在 0.4.7 就是 `idle + BACKEND_CONNECTED → connected`。
与 D06 同类：既有缺陷、非本次回归、可 Web OTA 热修。

### 发布能力缺陷：移动端 web OTA 被 native 更新分支短路（真机确认，2026-08-19）

**这一条推翻了发布信心模型的第一根支柱在移动端的适用条件。**

`mobile/plugins/k2-plugin/android/.../K2Plugin.kt` 的 `performAutoUpdateCheck()` 是「先 native，
后 web」的直线结构，且 native 有更新时直接 `return`：

```kotlin
val nativeResult = fetchManifest(androidManifestEndpoints(channel))
if (nativeResult != null && shouldUpdate && Build.VERSION.SDK_INT >= minAndroid) {
    notifyListeners("nativeUpdateAvailable", data)
    return                       // ← web 分支永远到不了
}
val webResult = fetchManifest(webManifestEndpoints(channel))
```

**真机证据**（Redmi K40 Pro / Android 14 / 装 0.4.6 / 线上 `kaitu/android/latest.json` = 0.4.7）：

| 观测 | 结果 |
|---|---|
| logcat `K2Plugin` | `load: starting auto-update check` 之后**零后续日志** |
| 屏幕 | 顶部横幅「v0.4.7 已准备好安装 / Later / 立即更新」 |
| web manifest 请求 | **一次都没发过** |

两个 CDN 的 android manifest 均实测为 `0.4.7`（`d13jc1jqzlg4yt.cloudfront.net` 与 `d0.all7.cc` 同值）。

**推论链**：

1. 0.4.8 发布后 android manifest = 0.4.8 → 对 0.4.8 设备 `isNewerVersion` 为 false → 不短路
   → **web OTA 本次发布可用**。这条缺陷**不阻断 0.4.8**。
2. 但**发布 0.4.9 的 APK 那一刻**，manifest 变 0.4.9 → 所有仍在 0.4.8 的设备每次冷启动都短路
   → **web OTA 对它们永久失效**，直到用户手动装 APK。
3. native 更新是**用户可拒绝**的（截图里的 "Later"）。拒绝的用户从此收不到任何 web 热修。
4. 「Web OTA 覆盖 webapp 176 文件、可小时级热修」这个承诺，在移动端只在**没发过更新 APK**
   时成立——而需要热修的场景常常恰恰伴随发版。

语义上两者根本不互斥：native 更新是「建议装新 APK」（可拒绝、异步、要用户操作），
web OTA 是「静默修好当前这个版本」（无感、立即）。**用户拒绝升级时，web 热修反而更重要。**

修复在 `fix/ota-native-web-decouple`（含 iOS / 桌面同构性排查）。

**同时作废的既有判断**：设计文档与本矩阵此前称「移动端在野已经在轮询」——准确的说法是
**在野客户端轮询的是 native manifest，web 分支被短路挡在后面**。B05 用例的整个前提由此不成立。

### Android 真机 P0 + 功能面（Redmi K40 Pro / Android 14，2026-08-19）

设备到手时装的是 **0.4.6**（非预期的 0.4.7）。开工第一件事是 `adb pull` 出 base.apk
（23.7 MB，`versionCode 406`，签名 `579aad1a…`）——**"一次性资源"的约束由此解除**：
0.4.6 状态可随时重建，且本机有两个 AVD 可承接负向门类用例。新构建的
`release/0.4.8/Kaitu-0.4.8.apk`（`versionCode 408`）签名指纹与之**逐字节相同**，
故可覆盖安装保数据。

| 项 | 结果 |
|---|---|
| **P0 连接** | **PASS** — `startVpn`(configJSON 758B) → TUN `ipv4=10.0.0.2/24 ipv6=fd00::2/64 dns=[1.1.1.1,8.8.8.8]` → connecting→connected **1.6s**。中途一次 `reconnecting` 由 `Network available` 回调首触发引起，与 macOS 的 NWPathMonitor first-fire 同源，非缺陷 |
| **真实通路** | **PASS（端到端，非"看起来连上了"）** — 设备出口 `35.182.199.248`（AWS ca-central-1）与选中的 **CA 1782** 一致；本机对照出口 `171.100.183.8`（泰国）。`tun0` 双栈 IPv4+IPv6 均在 |
| **P0 断开** | **PASS** — UI 归位、出口 IP 回落本地、**`tun0` 已不存在**、前台服务摘除 |
| **D05 国家排除** | **PASS** — 7 国 24 节点（8+5+3+2+2+2+2），与 macOS 同值；勾选实时生效（背景节点即刻标注「自动选择已排除」）、徽章计数正确、「清除」按钮清空后**对话框保留**（与 `CloudTunnelList.tsx:598` 的 `onClear` 语义一致） |
| **D06 573 文案** | **FAIL，与 macOS 同形** — 展开态「连接失败」，折叠态 InlineErrorBar「⚠ **未知错误**」。双平台确认，构成修复后的对照基线 |
| **A02 升级保态** | **PASS**，见 A02 行 |

**方法学两条**（下次真机会再踩）：

1. **盲点像素坐标会污染被测状态**。前期用截图估算坐标点击，误选了节点、误关了对话框，
   一度把「清除按钮无效」当成 bug 上报的边缘。实际是**在对话框入场动画未完成时点击**。
   改用 `adb shell uiautomator dump` 后精确了——**Capacitor WebView 的 accessibility 树
   是完整暴露的**（含 `text` / `bounds` / `enabled` / `clickable`），真机 UI 断言应当以它为准，
   截图只作旁证。
2. **release APK 没开 WebView debugging**（`/proc/net/unix` 里无 `webview_devtools_remote_<pid>`），
   所以 localStorage 读不到，A02 只能用 UI 判据；桌面那种 16 键逐字节比对在移动端做不了。

**顺带发现（非本次回归，未修）**：账户页品牌横幅渲染为「**Kaitu**.io 开途」，
而 CLAUDE.md 规定**中文用户面禁用 "Kaitu" 裸词**。0.4.6 上同样存在。是否改属品牌决策。

**Kotlin 层已有、webapp 层缺的防护**：断开日志里出现
`onStatus from stale engine — ignoring` —— 原生层显式忽略来自旧 engine 的状态回调。
这正是 A08 在 webapp 层缺失的那类保护，说明该竞态在原生层已被意识到。
`fix/stale-poll-race` 等于把这道防护补齐到前端。

### 三个修复的真机闭环（Android，2026-08-19）

同一台设备、同一操作路径的前后对照。修复合入 main 后**重新构建 APK 并刷机**验证，
不是拿 agent 各自 worktree 里的产物。

| 修复 | 真机判据 | 结果 |
|---|---|---|
| **D06 错误文案** | 全排除 7 国 → 折叠态错误条 | 修复前「⚠ 未知错误」→ 修复后「可用节点已全部被排除，请调整国家/地区过滤」。**PASS** |
| **移动端 OTA 解耦** | 冷启动 logcat 两条 lane 是否都执行 | 修复前只有一行 `starting auto-update check` 零后续；修复后 `Auto-update [native] skipped: no newer version (remote=0.4.7 local=0.4.8)` + `Auto-update [web] skipped: manifest unavailable` 两条俱全。**PASS**。2026-08-20 补上**正向命中**（native 命中时 web lane 仍执行），见下方降版本探针专段 |
| **A08 陈旧轮询** | 竞态窗口 ≈ 轮询往返/轮询周期 | 真机**未刻意复现**（概率性）。由 9 个 vitest 用例覆盖，含 8 项变异验证 |
| **P0 回归** | 三个修复后连/断是否仍干净 | 自动选择连上，出口 `35.88.216.55`（AWS 俄勒冈）；断开后 `tun0` 消失、出口回落。**PASS** |

**产物真实性用独立判别式验证，不信"构建成功"**：解包 APK 的 `classes.dex`，
确认含 `planAutoUpdate` / `AutoUpdateStep` **且不含**修复前的字符串
`"Auto-update check failed"`。这一步的必要性来自一个真实的假绿——
`node_modules/k2-plugin` 是**拷贝而非符号链接**，改 `mobile/plugins/` 后直接跑
gradle 测的是陈旧拷贝，会 3 秒返回全绿。

**顺带补上的构建门缺口**：出 release APK 有两条路径，
`scripts/build-mobile-android.sh`（CI）与 `make build-android`（本地），
而 k2-plugin 的单测门只加在前者——本次三个修复的 APK 恰恰是本地构建的。
已在 Makefile 补齐（`7526195f`），位置必须在 `cap sync` 之后，否则同样测到陈旧拷贝。
插件单测实测 66 个（27 AutoUpdatePlan + 34 K2PluginUtils + 5 Minisign），0 失败。

**已闭环（2026-08-20）**：「native 有更新时 web lane 仍执行」这个**核心**场景，
一度以为真机上验不到——设备已是 0.4.8 而线上 android manifest 是 0.4.7，native lane
必然 skip。**用降版本探针解决，不需要 beta 频道发布，不写生产 CDN。**

手法：native lane 比的是 `versionName`（`K2Plugin.kt:913`），而 Android 判「装包
算不算降级」看的是 `versionCode`。二者是**两条独立通道**，所以只压 versionName、
versionCode 保持 408，就能让 app 自认 0.4.6（→ native lane 必然命中），而安装仍是
平装：**数据保留、不用 `-d`、不用卸载**。

版本串从**仓库外**注入，一个源文件都没改：

```groovy
// scratchpad/lowver.init.gradle —— ./gradlew -I <此文件> :app:assembleKaituRelease
p.extensions.getByName('androidComponents').finalizeDsl { dsl ->
    dsl.defaultConfig.versionName = '0.4.6'
}
```

`finalizeDsl` 在 build.gradle 求值之后、variant 创建之前跑，是 AGP 8 的官方钩子。
产物独立核验（没信 BUILD SUCCESSFUL）：`versionCode='408' versionName='0.4.6'`，
签名 `579aad1a…` 与设备上完全一致，`classes.dex` 含 `AutoUpdatePlan` ×2。
两个 APK 压缩后**字节数相同**（29376931）——旁证除版本串外无差异。

同一台设备（M2012K11C / Android 14 / SDK 34）、同一组生产端点、同一次会话内的三段对照：

| 步骤 | 装的是 | logcat |
|---|---|---|
| 1 对照 | 正式 0.4.8 | `[native] skipped: no newer version (remote=0.4.7 local=0.4.8)` + `[web] skipped: manifest unavailable` |
| 2 探针 | 代码 0.4.8 / vn 0.4.6 | **`[native] update available: 0.4.7`** + **`[web] skipped: manifest unavailable`** |
| 3 复原 | 正式 0.4.8 | 回到步骤 1 的两行，横幅消失 |

步骤 2 同时有用户可见证据：`uiautomator dump` 抓到横幅 `v0.4.7 已准备好安装` +
`稍后再说` / `立即更新`，与 logcat 的 native 命中对上。

**判别式是第二行的存在与否**：修复前的代码在 `notify native` 之后直接 `return`，
web lane 一行都不会打（2026-08-19 用真 0.4.6 release APK 在同一台设备上现场观测过，
web 端点零请求）。web lane 落在 `manifest unavailable` 是因为生产 `kaitu/web/latest.json`
当前是 403——**它落在哪个 reason 不重要，重要的是它落了**。

附带一个此前只有单测覆盖的实测：设备无网络时（`Active default network: none`），
native lane 失败后 web lane **照样执行**并留下日志——失败隔离在真机上成立。

### iOS 观测通道受限（D3 执行约束）

- `log stream` 本机版本不支持 `--device-udid`，真机日志流不可用
- `devicectl device copy from` 对 `appGroupDataContainer`（`group.io.kaitu`）与
  `appDataContainer` 均返回 `CoreDeviceError 7000`，错误文案 `File paths cannot contain '..'`
  具有误导性——本地目标路径干净（`/Users/david/k2-ios-logs/`）时同样失败
- 结论：**iPhone 侧的验证必须靠人在设备上操作 + app 内「上传日志」回传**，
  不能像桌面那样由 agent 直接驱动

> 另更正一条早先的错误归因：`tunnelState='disconnected'` 曾被我判为"隧道空闲/锁屏自动断开、
> `deploy-ios-device.sh` 缺预热步骤"。实际是**数据线接触不良**（用户告知）。脚本无缺陷。

### D08/D09 根因：过期后续订把整个周期补记到过去，付费用户拿不到时间（已修，`8f1daed0`）

**现象**（真机 iPhone，2026-08-19 15:57–15:58）：用户报"subscription is successful，
但有效期没有加上"。

**服务端事实**（center-1 + center-2 `app.log`，三笔交易分落两台）：

| 时间 | txn | `credited_seconds` | kind |
|---|---|---|---|
| 15:57:50 | 2000001221796060 | **39600（11 小时）** | renewal |
| 15:58:07 | 2000001221785963 | 0 | renewal |
| 15:58:19 | 2000001221775815 | 0 | renewal |

三笔全部 `code:0`，ledger 与 `UserProHistory` 审计行齐全，`expired_at` 从
**08-15 01:16** 推到 **08-15 12:16**——**落点仍在四天前**，用户侧当然毫无变化。

**日志会骗人**：`[creditAppleTransaction] user 9378 credited +0d` 里的 `+0d` 是
`int(creditSeconds/86400)` 取整，实际发了 11 小时。精确值只在
`subscription_credits.credited_seconds` 里。

**因果链**：沙盒 1 年订阅 = 1 小时；`2000001221648433` 08-15 00:16 首购，几次自动续订
全部覆盖 08-14~08-15；这些交易一直没被 `finish()`（设备上直到 08-19 14:47 才装上带
IAP 链路的 0.4.8）；app 一起来 `Transaction.updates` 把积压交易全部重放补记。
**今天没有产生新订阅行**——用户看到的"购买成功"是重放交易 verify 成功后
`setLastGrantedUser` 触发的成功态，不是一次新购买。

**这暴露了一个与沙盒无关的真 bug**：`applyRenewalCredit` 连 `now` 参数都没有，
base 只取 `max(expired_at, priorPeriodEnd)`。两者都在过去时，整个周期埋进过去。
这正是 spec 自己列的威胁 **T6（under-credit → 付费用户被锁在外面）**——而 spec 写的
两道防线（finish-gating + reconciliation）只保证交易被入账，不保证时间可用。
**T6 的漏洞不在捕获，在计算。** 生产触发条件：漏通知 / 交易积压 / 对账补记 + 用户权益
恰好已过期，与沙盒无关。

已修（`8f1daed0`，Apple 与 Stripe 两条渠道同时生效）：`base` 改取
`max(expired_at, priorPeriodEnd, now)`，与 `applyGiftCredit` 的 from-now-if-expired
对齐；`creditSeconds` 同步改为从实际入账基准起算，否则过期补记会把死区当成本次交易
买来的时长，虚高 clawback 账本与审计天数。`delta <= 0` 分支不动。
`TestApplyRenewalCreditFromNowWhenExpired` 已做变异验证。**需要部署 Center 才生效——
不在 0.4.8 客户端发布链路里。**

**D08 复测方法（当前沙盒订阅已作废）**：手上这个沙盒订阅所有交易都指向过去，
永远验不出"有效期正确增加"。必须**换一个全新的沙盒 Apple ID 做首购**——那条路走
`isFirst` 分支、`applyGiftCredit` 从当下起算 3600 秒，有效期会变成"1 小时后"，肉眼可验，
建单与分销返现也才会被触发（沙盒交易按设计不建单，D08 的建单断言需要生产交易或
mock 环境，见 `api/CLAUDE.md` 的 `iapOrderFixture` 段）。

## 阶段 C — k2 核心回归（`ef6c0b2 → 6bc70b0`，上轮 UAT 未覆盖段）

| ID | 用例 | 对应变更 | Status |
|----|------|----------|--------|
| C01 | IPv6 连接的进程归属非空（三平台） | `f4ac8e1` 拆除 `!Is4()` 早退门 | **PASS（darwin+shared）** 2026-08-21 — `IPv6ParticipatesInLookup`、`IPv6Attribution_RealProcess`、`ParseLsofIndex_IPv6Bracketed`、`ParseSOCKS5UDPHeader_IPv6` 全绿。**Linux/Windows 平台测试受 build constraint 在 macOS 不编译**，那两平台的 `!Is4()` 拆除需各自平台跑 process_linux/windows_test.go |
| C02 | 强直连/强代理 app 的 DNS 跟随进程规则视角 | `d343bf6`（R2） | **PASS（darwin+shared）** 2026-08-21 — core/dns meta 5 测试绿（view override、跨 view 缓存隔离、reject 仍优先、nil meta 默认 view）、engine dns_handler meta + rule dns_meta 全绿（process/package/installer 规则匹配、combined-port fail-closed）。Windows/Android meta 用例受 build constraint SKIP，需各平台 |
| C03 | macOS 连接建立无延迟退化（进程归属改 lsof 全表快照） | `17f62f7` | **PASS** 2026-08-21 — provider 全套绿，核心判别式 `TestDarwinProcessSearcher_IndexBuiltOncePerTTL_NotPerConnection`（索引每 TTL 建一次而非每连接 shell-out，延迟退化根因被消除）+ `ConcurrentBuildsCollapse`；真机 lsof 集成测试 `K2_REAL_LSOF=1` PASS：本机真 lsof 扫出非空索引且自身 socket 按 selfPid 正确排除（found=false 即判别式） |
| C04 | pin mismatch 不再永久污染 UI 归因 | `023b5c9` | **PASS** 2026-08-21 — `TestEngine_ClearWireError_ClearsPinMismatch` + `TestEngineError_RequiresUserAction` + wire `TestRelayFetch_NonOKStatusPassthrough` 全绿。（跑测试需临时还原 k2 的 gomobile 脏 go.sum，跑完字节级还原） |
| C05 | `rule diagnose` / `audit` 工具可用（diagnose 曾对任何域名都报 DIRECT） | `61fd8bf` | **PASS** 2026-08-21 — 实跑 `TestDiagnose -host=google.com`：加载 20 bundles/40 sets，命中 `overseas`→全 17 国 **PROXY**（修复前空规则集全 DIRECT），判别式（bundles==0‖idx==0→Fatal）在位；`TestAudit_GeositeCrossCheck`（`-tags audit`）编译并 PASS（修复前根本编译不过） |
| C06 | ECH config_id gate + ECH-block probe | `aa744bf` / `cbdccc9` | 代码侧已排除断连风险（见"已排除的风险"）；仍需一次真实连接确认 |
| C07 | 双通道记分板回归（M03 / canary C03 家族） | — | TODO |
| C08 | SOCKS5 UDP 代理模式 full-cone | `7682a65` | **PASS** 2026-08-21 — `TestUDPRelay_OneSessionManyDestinations`（单 session 服务任意多目的地 = full-cone 特征，非对称 NAT 做不到）+ `DistinctSourcesGetDistinctSessions` + `WriteTo_UsesAddrArgument` 全绿 |

---

## 阶段 E — 认证与计量链路

| ID | 用例 | 期望 | Status |
|----|------|------|--------|
| E01 | `/api/subs` 下发内嵌 tunnel token，滚动续期 | 客户端采用 tunnel token 而非 access token | **部分 PASS** 2026-08-21 — 活体桌面 daemon `status` 的 config 用 `k2v5://…@` token，解码 payload `type:tunnel`（user 2162，签发 2026-08-14，exp 2026-11-16），确证客户端采用 tunnel token 非 access token。滚动续期（token_issue_at 推进）需长时观测或真连接，未闭环 |
| E02 | 移动端 `updateConfig` 把续期后的 token 同步进 App Group / SharedPreferences | 续期后隧道不掉线 | TODO |
| E03 | tunnel token **不能冒充** access token 调用 `/app/*` | `handleJWTAuth` default-deny 生效 | **PASS** 2026-08-21 — `TestHandleJWTAuth_RejectsTunnelToken` 绿：tunnel 类型 token 被 handleJWTAuth default-deny 拒绝，不能冒充 access token 走 `/app/*` |
| E04 | 被封禁用户 → 403；节点认证失败与设备认证失败可区分 | `1a500ccd` / `8a11d362` | **PASS（服务端逻辑）** 2026-08-21 — handler 测试 7 子测试全绿：节点认证失败（缺 BasicAuth/未知节点/密钥不符）返回系统错误**非 401**、设备认证失败仍 401、tunnel token 200、access token 过渡期 200、anchor bump→401、**封禁用户 403**。断言锚在各自那道门，不涉下游 |
| E05 | per-device 流量上报 → 后台 `/manager/usages` 排行 + 用户详情有数 | `e6612273` / `8c856146` | **PASS（服务端逻辑）+ 活体未闭环** 2026-08-21 — ingest 测试全绿：upsert 幂等（cursor 去重）、user 解析、wire 形状、session-daily。**活体 per-device 数据流用现有只读 MCP 确认不了**（`usage_overview` 只给节点级聚合，per-device 流量表未暴露）；需 `/manager/usages` 页面或 DB 查。Center 本身活着（日活 ~450） |
| E06 | 超量告警邮件（阈值 100GB，月度去重） | `3f57e1c8` / `38f16a6c` | **PASS（服务端逻辑）** 2026-08-21 — worker 测试全绿：find+月度 dedup、默认阈值 100GB、180d 保留清理（含 device-session-daily）。实际发信+月度去重的活体需 worker 在生产跑一个周期，属 center-deploy 范畴非客户端发布链路 |
| E07 | **（可选，高风险）** 单节点开 `K2_ENFORCE_AUTH=1` 灰度验证强制路径 | 新客户端连得上、存量 0.4.7 被正确拒绝或放行（按设计） | 建议**发布后**单独排期 |

---

## 阶段 P — 发布收尾

| ID | 项目 | Status |
|----|------|--------|
| P01 | `publish-desktop.sh` 切 `cloudfront.latest.json` / `d0.latest.json` / Linux `LATEST` | TODO |
| P02 | `publish-mobile.sh` 切 android manifest（iOS 待审核批准后单独执行） | TODO |
| P03 | 从 0.4.7 真机验证 auto-update 拉到 0.4.8（桌面 + Android + Linux 各一台） | TODO |
| P04 | k2s 车队是否随本次核心变更同步升级 | **建议本次不动**——服务端 `ef6c0b2` 已稳定 3 天，客户端与服务端不要同时变更，否则出问题无法二分。待客户端稳定后单独排期 |
| P05 | iOS 重新提审（build 号 **4408990**，正式版 `IOS_BUILD_REV=0`；被拒后重传才 bump REV），批准后单独上架 + 切 ios manifest | TODO |

## 发布信心模型（诚实版）

**"每条变更都被直接测过"意义上的 10/10 不可达**——412 个提交，任何清单都是抽样，组合效应测不完。声称 10/10 是自欺。

**可达的是"任何未测到的问题都有已验证的补救通道"**，这才是发布信心的正确定义。四条补救通道：

| 通道 | 覆盖 | 前置条件 |
|---|---|---|
| **Web OTA** | webapp 176 文件（最大变更面）——UI/逻辑层问题可小时级热修，不重发客户端 | **阶段 B 必须先全绿** |
| `K2_CHANNEL_SELECTOR_LEGACY` | wire 双通道选择器回滚阀 | 已存在 |
| `K2_ENFORCE_AUTH=0` | tunnel token 全链路是软路径，坏了自动退回 access token | 已是现状 |
| 节点侧不动（P04） | 服务端保持已生产验证 3 天的 `ef6c0b2` | 编排决策 |

**唯一没有补救通道的是移动端原生层**（K2Plugin +220 行、iOS NE/entitlements、minisign）——坏了只能重新发版过审。因此 **A07/A09/B04/D08 权重最高**，建议移动端先走 beta 通道小流量。

按此模型，跑完阶段 0/A/B/C/D 后的目标态：
- **代码信心 9.5**（CI 全绿 + 契约门 + 三层阶梯 UAT + 生产服务端 3 天）
- **业务信心 9**（补救通道全部验证可用；剩余 1 分是 GFW 下 Chrome-146 指纹的真实表现——地域性、概率性，本地真机测不出，只能靠灰度观察）
