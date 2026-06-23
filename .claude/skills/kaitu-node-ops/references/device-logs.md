# Device-log triage & DIAG analysis

Reference for `kaitu-node-ops`. Use when a user reports a problem and you need to find + analyze their uploaded device logs.

## MCP tools

| Tool | Purpose | Params |
|------|---------|--------|
| `query_feedback_tickets` | Find user feedback tickets | `udid?`, `email?`, `user_id?`, `status?`, `from?`, `to?` |
| `query_device_logs` | Find log uploads | `udid?`, `user_id?`, `feedback_id?`, `reason?`, `from?`, `to?` |
| `download_device_log` | Download + decompress from S3 | `s3_key` (from query results) |
| `resolve_feedback_ticket` | Mark resolved | `id`, `resolved_by` |

## Workflow

1. **Identifier** — ask for UDID, email, or user ID.
2. **Tickets** — `query_feedback_tickets(email=…)` / `(udid=…)`.
3. **Logs** — `query_device_logs(feedback_id=…)` using the ticket's feedbackId.
4. **Download** — `download_device_log(s3_key=…)` for each file (service/crash/desktop/system). Saves to `/tmp/kaitu-device-logs/`.
5. **Quick diagnosis** — `bash scripts/k2-quick-diag.sh <k2.log>` (auto-extracts DIAG events/heartbeats/failures, verdict OK/WARN/CRITICAL/PANIC). **Always run this first.**
6. **Deep dive** — DIAG grep patterns below.
7. **Resolve** — `resolve_feedback_ticket(id=…, resolved_by="claude")`.

Logs are gzip on S3 (`download_device_log` auto-decompresses); large logs truncate to 50k chars (focus on end-of-file); time filters use RFC3339. Cross-reference `feedback_id` between tickets and logs.

| Log type | Content |
|------|---------|
| `service` | Go daemon (k2.log) — VPN connection, wire protocol, engine |
| `crash` | Go panic (panic-*.log) — stack traces |
| `desktop` | Tauri (desktop.log) — IPC, updater, tray, upload |
| `system` | OS logs filtered for kaitu |

Upload reasons: `user_feedback_report` (SubmitTicket page), `beta-auto-upload` (24h beta auto).

## DIAG log analysis (k2 client)

All client diagnostics use the `DIAG:` prefix (three-layer system). Triage:

```bash
grep "DIAG: heartbeat" <logfile> | tail -20      # 1. health: health/transport/loss/rttMs/txMB/rxMB/tcpConns/udpConns/uptimeS/fallback (every 30s)
grep "DIAG:" <logfile> | grep -v heartbeat       # 2. problems: dns-slow/dns-fail/proxy-dial-fail/transport-switch/wire-error
```

Layer drill-down:

| Layer | grep | Shows |
|-------|------|-------|
| Connection | `DIAG: connected\|DIAG: session-end` | session lifecycle + total traffic |
| DNS | `DIAG: dns` | slow (>500ms) / failed queries |
| Transport | `DIAG: quic\|DIAG: transport` | QUIC handshake fail, QUIC↔TCP-WS switch |
| Proxy | `DIAG: proxy-dial` | failed / slow (>3s) proxy dials |
| Wire | `DIAG: wire-error` | classified engine errors (auth/timeout/unreachable) |
| Health | `health: degraded\|health: critical` | state transitions |

| Event | Level | Meaning |
|-------|-------|---------|
| `heartbeat` | INFO | 30s health snapshot |
| `connected` | INFO | tunnel established (server, mode, dial time) |
| `session-end` | INFO | tunnel torn down (uptime, tx/rx) |
| `dns-slow` / `dns-fail` | INFO/WARN | DNS >500ms / upstream failed |
| `proxy-dial-fail` / `proxy-dial-slow` | WARN/INFO | wire proxy dial failed / >3s |
| `quic-handshake-fail` | WARN | QUIC failed (UDP maybe blocked) |
| `transport-switch` | WARN | QUIC→TCP-WS or back |
| `wire-error` | WARN | classified engine error (code/category) |

Diagnosis patterns:

| Heartbeat symptom | Likely cause | Next |
|---------------------|-------------|------|
| `health=degraded`, `loss>0.05` | packet loss | check `quic-handshake-fail` count |
| `health=critical`, `loss>0.25` | severe loss / UDP block | check `transport-switch` for fallback |
| `fallback=true` | QUIC blocked, on TCP-WS | check `quic-handshake-fail` root cause |
| `tcpConns=0`, `udpConns=0` | no traffic | check `proxy-dial-fail` |
| `rttMs` >500 | high latency | may be normal for distant servers |

## Quick-diag script

```bash
bash scripts/k2-quick-diag.sh /tmp/kaitu-device-logs/k2.log   # downloaded log
bash scripts/k2-quick-diag.sh                                 # local daemon (auto-detect macOS/Linux path)
```

Outputs: session info · last 5 heartbeats · problem events · event-type counts · health transitions · panics · verdict (OK/WARN/CRITICAL/PANIC).
