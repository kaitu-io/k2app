---
name: kaitu-support
description: Use when triaging Kaitu VPN feedback tickets (无法连接 / 速度慢 / 应用受影响 / App 卡死 / 订阅失败). Maps user symptoms to specific k2 DIAG log patterns, distinguishes client-only vs server-required diagnoses, and enforces confidence floors for performance complaints.
triggers:
  - support ticket
  - feedback ticket
  - user issue
  - device log
  - diagnose
  - troubleshoot
  - customer support
  - 无法连接
  - 速度慢
  - k2 log
  - DIAG
---

# Kaitu Technical Support

Use this skill when triaging user feedback tickets. All operations use kaitu-center MCP tools.

## Available Tools

| Tool | Purpose |
|------|---------|
| `lookup_user` | Find user by email or UUID |
| `list_user_devices` | List user's registered devices |
| `query_device_logs` | Find device logs in S3 (filter by `feedback_id` to link to a ticket) |
| `download_device_log` | Download + extract log files to `/tmp/kaitu-device-logs/` |
| `query_feedback_tickets` | Search feedback tickets |
| `list_ticket_replies` | List all replies on a ticket |
| `reply_feedback_ticket` | Reply to user (triggers aggregated email after 5min) |
| `resolve_feedback_ticket` | Mark ticket as resolved |
| `close_feedback_ticket` | Close ticket (not actionable) |

## Triage Workflow

### Step 1 — Identify the Ticket
```
query_feedback_tickets(id=<ticket_id>)
```
Extract: `userId`, `email`, `meta.os`, `meta.appVersion`, `meta.vpnState`, `content`, `logCount`.

### Step 2 — User Context
```
lookup_user(uuid=<user_uuid>)        # membership, plan, cloud instance
list_user_devices(uuid=<user_uuid>)  # UDID list, app versions, last seen
```

### Step 3 — Read Prior Replies
```
list_ticket_replies(id=<ticket_id>)
```
Avoid duplicate responses.

### Step 4 — Pull Device Logs
```
query_device_logs(feedback_id=<feedback_uuid>)   # preferred — links log to this ticket
download_device_log(s3_key=<key>)                # extracts to /tmp/kaitu-device-logs/
```
Desktop logs: `desktop.log` (Tauri/Rust shell) + `system--k2.log` (Go core — **DIAG events live here**).
Mobile logs: platform-specific, same DIAG events.

### Step 5 — Quick Diag
```bash
bash scripts/k2-quick-diag.sh /tmp/kaitu-device-logs/<dir>/system--k2.log
```
Run this FIRST — it prints last heartbeat, DIAG event counts, health transitions, panics.

### Step 6 — Symptom-Driven Investigation

Pick the row matching the user's complaint. Run the greps against `system--k2.log` (or mobile equivalent).

| User symptom | Primary grep | What to conclude |
|---|---|---|
| **无法连接** | `grep "DIAG: transport-race-fail\|wire-handshake-fail\|wire-error\|DIAG: connected"` | No `DIAG: connected` after attempts → never handshaked. `transport-race-fail` with all three of `quic443Err`/`quicHopErr`/`tcpwsErr` populated → all transports blocked (likely GFW escalation or ISP). `wire-error` code 570/503 → server-side; 401/402/403 → account/auth. |
| **速度慢** | `grep "DIAG: heartbeat" \| tail -20` + `grep "DIAG: dns-slow\|proxy-dial-slow\|udp-relay-timeout\|transport-switch"` | Heartbeat `loss`/`rttMs`/`fallback` tell the story: `fallback=true` = TCP-WS degraded, `loss>0.05` = lossy link. Many `udp-relay-timeout` or `transport-switch` = UDP hostile network. **Client-only caps at 5/10 — MUST do §8 server-log correlation to go higher.** |
| **连接不稳定** | `grep "DIAG: wake\|transport-rerace\|echo-probe-fail\|transport-switch"` | `DIAG: wake sleepS=...` → system sleep caused the break (expected). `transport-rerace` with `reason=3-echo-fails` → link silently died, re-raced. Repeated `transport-switch` QUIC↔TCP-WS → flaky UDP path. |
| **微信/WhatsApp/通话受影响** | `grep "DIAG: dns-proxy-\|udp-relay-timeout\|DIAG: proxy-dial-fail"` and filter by `dest=` matching the app's domain | Voice/video need UDP: `udp-relay-timeout` correlated to the app's relay hosts = UDP starved. Many `dns-proxy-recv-no-callback` = DNS proxy overloaded, domains never resolved. `proxy-dial-fail` on specific `dest` = rule routed that host wrong. |
| **App crash / VPN 把手机卡死** | `grep -i panic` then `grep "DIAG: heartbeat" \| tail -5` and `grep "DIAG: pipe-watchdog"` | Panic stack → CLIENT_BUG. Heartbeats suddenly stop (no 30s tick) = daemon hung. `pipe-watchdog firstExitDir=...` = stuck half-closed pipe was force-closed. For iOS "梯子卡住导致手机断网" — the Network Extension didn't cleanly tear down; check `session-end` is logged. |
| **订阅 / 节点刷新失败** | `grep "DIAG: subs-refresh-fail"` + check Center `/api/subs` directly | `endpoint`+`err` fields are definitive. If endpoint returns 5xx → SERVER_ISSUE; if TLS handshake fails → network / GFW block on the subs domain. |
| **[Auto] bad connection experience** | Ticket body already names `Server`, `Duration`, `Rule`. Grep the last `DIAG: session-end` and the 20 events before it. | **Semantics**: `[Auto]` = user **manually clicked disconnect** AND rated session bad on the post-disconnect prompt. The tunnel **did successfully connect** (otherwise no rating prompt) — `Duration` is how long the user actively used it before deciding it was bad. Bad rating reflects **subjective in-session experience**, not connect failure. **Reads**: `Duration=0–10s` repeated across multiple tickets = user connected, immediately found nothing worked, disconnected — most likely cause is post-handshake breakage (e.g., DNS-via-proxy timing out, `proxy-dial-fail` storm, `udp-relay-timeout`). `Duration` minutes-to-hours = quality degraded mid-session — check heartbeat trends, `transport-switch`, `transport-rerace`, server-side k2s.log loss. **Do NOT** classify these as "couldn't connect" — connect succeeded; the issue is what happened after. |
| **登录失败 / 收不到验证码** | **Don't read k2 logs** — this is Center API, not k2 tunnel. **Invoke the `center-ops` skill** to grep Center app logs for `/api/auth/code`, `/api/auth/login`, `/api/auth/web-login` on the user's email. **MUST query BOTH center-1 (35.77.181.30) and center-2 (13.230.22.35)** — ALB load-balances, the record may live on either server, not both. Also check `lookup_user` for `isFirstOrderDone` / `isActivated` to see if the account eventually did log in and purchase. | If no matching record on either server → code truly wasn't sent (check rate-limit / bounce). If code was 200 + login succeeded later → transient provider delay (especially @qq.com / @outlook), often self-heals. If user insists they didn't get code but log shows 200 → confirm email spelling (typo like wrong prefix / wrong domain); common cause: friend-reports-for-friend mix-up. |
| **已付款但会员未到账 / 能退吗我微信支付的** | **Don't read k2 logs** — payment flow, not tunnel. Follow `reference_wordgate_webhook_integration.md` in memory. Core query: `JOIN kaitu.orders k ON wordgate.orders w` 找 `w.is_paid=1 AND (k.is_paid=0 OR NULL)`. Grep `/apps/wordgate/wordgate.log` 看 WordGate 那边是否收到 Stripe 通知。Grep Kaitu `/apps/kaitu/logs/app.log` **两台都查** — ALB 轮询，webhook 只落其中一台。关键 reqId 链：`[Webhook] received → MarkOrderAsPaid → addProExpiredDays → status:500/200`。| WordGate 付款成 + Kaitu 未到账 = webhook 没处理完。两种根因：(a) Kaitu 返 5xx（binary 有 bug / schema 漂移 / 死锁）+ WordGate SQS fallback 凭据 `AKIASWWJ4TKXW7XCPUGP` 已 `InvalidClientTokenId` → 通知永久丢；(b) 真的没发（少见，看 WordGate log）。处理：先修 Kaitu 侧根因，再用 reference memory 里的 `curl` 模板 replay webhook（state-changing，走 `center-deploy`）。Replay 是幂等的（`FOR UPDATE` + `localIsPaid` 检查）。**不要直接改 DB** — 会跳过返现/邀请奖励/tier 同步。 |
| **macOS 11.x 不支持** | **Don't read logs.** | Tauri v2 requires macOS 12+. Reply with the supported range; no fix. |

### Step 7 — Confidence Ladder (MANDATORY)

Confidence is a function of **how many independent sources confirm the same root cause**. State current tier and max-reachable-tier before replying.

```
Tier 1 (≤ 5/10)  Client log only
Tier 2 (≤ 7/10)  + Server k2s.log from §8 covering the same time window, same user IP
Tier 3 (≤ 9/10)  + Code read at user's exact commit/version from §9, symptom reproduces in code path
Tier 4 (= 10/10) + Panic stack trace pointing to a specific line in §9-resolved code
```

**Per-symptom caps on top of the ladder:**

| Symptom | Max tier without server log | Rationale |
|---|---|---|
| 速度慢 / 不稳定 / 丢包 | **Tier 1 only (5/10)** | Client `rxMB` = direct + proxy mixed; client `loss` = uplink only. Downlink truth lives in `k2s.log`. |
| 无法连接 with clear `transport-race-fail` (all 3 transports err set) | Tier 2 (7/10) client-only OK | All-transport failure is conclusive GFW/ISP signal; server log adds little. |
| 无法连接, partial evidence | Tier 1 (5/10) | Need server log to confirm client never reached node. |
| 微信/WhatsApp 受影响 | Tier 1 (5/10) | UDP starvation may be server-side; must cross-check k2s.log. |
| App crash / panic | Tier 4 possible if stack + code match | Panic is self-contained evidence. |
| Login / verification code | Up to 9/10 from Center API logs + DB | Not a k2 issue — different evidence chain. |
| 已付款未到账 | Up to 10/10 from `kaitu.orders ⟷ wordgate.orders` JOIN + WordGate + Kaitu logs with full reqId trace | Two authoritative DBs; root cause usually visible in one side's log. See `reference_wordgate_webhook_integration.md`. |
| No evidence | UNKNOWN — ask user for specifics, do NOT resolve |

**Hard rules:**
- Below Tier 2 for any non-panic complaint → MUST escalate to §8 before resolving.
- Below Tier 3 for any reply that names a code-level cause → MUST do §9 first.
- Stay UNKNOWN if evidence doesn't triangulate. `claude-support` ≠ `claude`; resolving at UNKNOWN is a process violation.

### Step 8 — Server-Side Log Correlation (§8 escalation)

Invoked when Tier 1 is insufficient. Requires `kaitu-node-ops` skill for node ops.

**8.1 — Identify the node**
```
# From client log
grep "DIAG: connected" system--k2.log | tail -5
# → server=www.<province>.people.cn  ← SNI cover, NOT real geography
```
Resolve real node: `list_nodes()` (kaitu-center MCP), match the `server` domain to a node record, take `ip` field. The province-cover domain maps to a specific node via `tunnels[].sniDomain`.

**8.2 — Identify the time window + user IP**
From client log: connect time (`DIAG: connected` timestamp) and session end (`DIAG: session-end`). Convert to node's timezone (most nodes: UTC or local TZ from `list_nodes` metadata).

`k2s.log` is indexed by **client public IP**, not UDID. If the user IP isn't known:
- Check if ticket meta leaked it (rare)
- Cross-match by DNS-fingerprint timing: pick a uniquely-timed DNS query from client log (e.g. a 02:14:23.451 lookup for a rare domain), grep `k2s.log` in the ±2s window for a matching incoming request. See memory `reference_udid_to_public_ip.md`.

**8.3 — Pull the real k2s.log**
Do NOT use `docker logs k2s` — that's only the 5-line startup tail (see memory `reference_k2s_log_location.md`). Real log: `/apps/kaitu-slave/logs/k2s.log` on the node.

```bash
# Via kaitu-node-ops (exec_on_node):
exec_on_node(
  ip=<node-ip>,
  command="grep -E '<user-ip>' /apps/kaitu-slave/logs/k2s.log | awk '$0 >= \"<YYYY-MM-DDTHH:MM:SS>\" && $0 <= \"<YYYY-MM-DDTHH:MM:SS>\"' | head -200"
)
# For rotated days:
exec_on_node(ip=<node-ip>, command="zgrep '<user-ip>' /apps/kaitu-slave/logs/k2s-*.log.gz | head -100")
```

**8.4 — Read what matters**
- `mode=app-limited loss=0` → user connected but never pushed real traffic (not a VPN problem — see memory `reference_k2cc_app_limited.md`)
- `loss > 0.05` on the server side during the session = real downlink degradation (SERVER_ISSUE or user's ISP path)
- Any `wire-error` emitted server-side → already the cause, no more digging needed
- **Ignore sidecar metrics** — `netIn`/`conn` in sidecar logs reflect the sidecar itself, not user traffic (memory `reference_sidecar_metrics_misleading.md`)

Raise confidence to Tier 2 when client + server agree on the same fault window. If they disagree (client says loss, server says clean), that IS the finding — it localizes the problem to the path between them (usually user's ISP).

### Step 9 — Code at User's Exact Version (§9 escalation)

Invoked when you need to read code to confirm a bug path. Don't read HEAD — it may have diverged from what the user is running.

**9.1 — Identify the version**
From ticket `meta`:
- `meta.appVersion` → e.g. `"0.4.3"` (always present)
- `meta.commit` → e.g. `"9e12d0b"` (present on 0.4.2+ builds)

Client log also stamps the build at startup — `grep -i "build\|version\|commit" system--k2.log | head -5`.

**9.2 — Read code without mutating the submodule**
The k2 submodule is read-only from the parent worktree. Do NOT `git checkout` inside `k2/` from here. Use one of:

```bash
# A. Read a single file at the user's commit (preferred, zero state change)
cd k2 && git show <commit>:engine/health.go | less

# B. Grep across files at the user's commit
cd k2 && git grep "DIAG: transport-rerace" <commit>

# C. Temporary worktree at the user's commit (for larger investigations)
cd k2 && git worktree add /tmp/k2-at-<commit> <commit>
# ... read files under /tmp/k2-at-<commit>/ ...
cd k2 && git worktree remove /tmp/k2-at-<commit>
```

If `meta.commit` is empty (legacy clients): `git show v<appVersion>:path` using the release tag, or find the commit from `git log --grep "release <appVersion>"`.

**9.3 — Trace the symptom**
Given a DIAG event name, grep the commit to find emit site and callers:
```bash
cd k2 && git grep -n "DIAG: <event-name>" <commit>
```
Cross-reference with the architecture map in `k2/CLAUDE.md` at that commit.

**9.4 — Bug is fixed on main?**
```bash
cd k2 && git log --oneline <commit>..HEAD -- <file-with-bug>
```
If a fix commit exists post-`<commit>`, classification becomes KNOWN_FIXED; identify the release it shipped in (`git tag --contains <fix-commit>`) and tell the user which version to upgrade to.

### Step 10 — Reply

```
reply_feedback_ticket(id=<ticket_id>, content="...")
```

Guidelines:
- Write in the user's language (detect from ticket content).
- Be concise: state the problem, then solution/workaround.
- Include specific version numbers when recommending an upgrade.
- **NEVER expose internal infra details** — no server IPs, stack traces, DIAG event names, error codes, node hostnames.
- If user action is required, give clear step-by-step instructions.

### Step 11 — Resolve or Close

| Situation | Action |
|-----------|--------|
| Diagnosed, reply sent | `resolve_feedback_ticket(id, resolved_by="claude")` |
| Fixed in later version | Reply with version info → `resolve_feedback_ticket` |
| Not actionable / spam / feature request out of scope | `close_feedback_ticket(id)` |
| Cannot determine, need more info | Reply asking specifics, do NOT resolve yet |

### Step 12 — Cleanup
```bash
rm -rf /tmp/kaitu-device-logs/<extract-dir>/
```

## k2 DIAG Event Reference (cheat sheet)

Three layers in `system--k2.log`:

- **Heartbeat** (every 30s): `DIAG: heartbeat health=... transport=... loss=... rttMs=... fallback=... heapMB=... goroutines=...`
- **Events** (threshold-gated): `DIAG: <kebab-name>` with context fields
- **DEBUG** (off by default): full per-operation trace

Full reserved event table lives in `k2/CLAUDE.md` § Diagnostic Logging Constitution. Common ones surface-relevant for support:

- Connection: `connected`, `session-end`, `transport-race-start/winner/fail`, `wire-handshake`, `wire-handshake-fail`, `wire-error`
- Runtime health: `heartbeat`, `wake`, `transport-switch`, `transport-rerace`, `echo-probe-fail`
- DNS: `dns-slow`, `dns-fail`, `dns-proxy-timeout`, `dns-proxy-recv-no-callback`, `dns-proxy-conn-dead`
- Proxy: `proxy-dial-fail`, `proxy-dial-slow`, `udp-relay-timeout`
- Subs / misc: `subs-refresh-fail`, `pipe-watchdog`, `datagram-readloop-exit`

## Diagnostic Anti-Patterns (do not make these mistakes)

### "节点远 → 速度慢 / 卡顿" — WRONG

**Distance to a node only adds RTT (latency); it does NOT cause throughput loss, app hangs, or pipe-watchdog bursts.** Kaitu's k2cc congestion control + BBR are designed for long links — a 200ms-RTT path to AU is not "slower" than a 60ms-RTT path to HK in terms of bandwidth or reliability. RTT 100ms vs 250ms is a UX nuance for interactive apps, not a root cause for connectivity failure.

**Never** recommend "switch to a closer node" as a fix for:
- Slow downloads / video buffering
- Apps not loading
- pipe-watchdog / proxy-dial-fail bursts
- Goroutine spikes
- "VPN connected but nothing works"

If the heartbeat shows healthy `loss=0` and stable `rttMs`, distance is **not** the cause — keep digging. Real causes for those symptoms: server-side egress saturation, destination geo-blocking, GFW interference on the specific path, or app-side issues. None of those are fixed by picking a node 100ms closer.

Acceptable distance-related advice is narrow: only mention RTT when the user's complaint is **explicitly** about interactive latency (gaming ping, voice call lag, SSH responsiveness) — and even then, frame it as "lower RTT improves *interactive feel*", not "fixes speed".

## Classification Guide

| Classification | Meaning | Reply Template |
|---------------|---------|----------------|
| CLIENT_BUG | Bug in app code (panic, logic error) | Acknowledge + workaround if any + "will fix in next version" |
| CLIENT_CONFIG | User config issue (wrong mode, wrong server) | Step-by-step fix instructions |
| SERVER_ISSUE | Server/node problem (confirmed via k2s.log) | "We've identified the issue and are working on it" |
| NETWORK | User's ISP / network (GFW, ISP throttling, captive portal) | Network troubleshooting — restart router, try different network |
| KNOWN_FIXED | Fixed in later version | "Please update to version X.Y.Z" |
| PLATFORM_UNSUPPORTED | macOS <12, old iOS, etc. | State supported range |
| NOT_K2_ISSUE | Login / verification code / account / billing | Route to Center API diagnosis, not k2 logs |
| UNKNOWN | Cannot determine | Ask user for specifics — do NOT resolve |

## Safety Rules

- NEVER expose internal details to users (server IPs, DIAG events, stack traces, error codes, node names).
- NEVER modify code during diagnosis — read-only analysis only.
- Follow the §7 confidence ladder. Do not resolve below Tier 2 for non-panic complaints.
- Always check `list_ticket_replies` before replying to avoid duplicates.
- Clean up `/tmp/kaitu-device-logs/` after finishing.
- 登录 / 验证码 类工单不要花时间读 k2 日志 — 和 k2 核心无关。
