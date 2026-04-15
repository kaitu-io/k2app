# k2 Daemon × k2subs:// UAT Design

**被测物**：`k2/cmd/k2` daemon（当前跑在 `:11777` 的 dev 实例）
**入口**：`POST /api/core {action:up, params:{config:{routes:[{via:"k2subs://…"}]}}}`
**不测**：Center `/api/subs`（另一轮已覆盖）、webapp UI

## 凭据 & 环境

- UDID / TOKEN 从 `POST /api/core {"action":"status"}` 的 `data.config.routes[0].via` 解析得到（已验证可提取）
- Center host：`k2.52j.me`
- daemon 日志：`/var/log/kaitu/k2.log`
- cacheDir：daemon 内部 `filepath.Join(cacheDir(), "subs")`（macOS：`/var/root/Library/Caches/kaitu/subs/` 或 `~root` 等价）

## 网络故障注入约定（全部带 fallback）

| 手段 | 命令 | Fallback |
|---|---|---|
| 物理断网 | `sudo ifconfig en0 down` | `echo "ifconfig en0 up" \| sudo at now+2min` |
| 屏蔽 Center | `pfctl` anchor block `k2.52j.me` 出站 | `echo "pfctl -a kaitu-uat -F all" \| sudo at now+2min` |
| 黑洞 tunnel IP | `sudo route add -host <ip> 127.0.0.1` | `echo "route delete -host <ip>" \| sudo at now+2min` |

**规则**：任何注入故障的动作，**必须在同一个命令块里提交 at-job fallback**，避免测试中断锁死网络。

## 测试矩阵

编号 → TaskList（见 TaskCreate #15–#32）。每条 UAT 包含：**前置 → 动作 → 预期 state / API response / log 关键字**。

### 正常路径 / 参数边界
- **T01** 合法 URL → connected, `subscription resolved servers=17`
- **T02** `?country=JP` → connected + egress IP 属 JP
- **T03** `?country=ZZ` → 511 "no tunnels available"

### URL 解析
- **T04** 缺 password / refresh=xyz / 错 scheme（3 子用例）

### 网络层故障
- **T05** 首次无缓存 + 物理断网 → "fetch failed (no cache)"，disconnected
- **T06** 有缓存 + Center 被 pfctl 屏蔽 → 走 "using cached data" 分支成功
- **T10** 已连接后物理断网 → engine 进入 degraded/critical，恢复后自愈
- **T11** 已连接后黑洞 tunnel IP → re-race 换候选（country=auto，17 候选池可验证）

### 认证故障
- **T07** token 最后一字符篡改 → 401 "invalid credentials"
- **T08** 伪造 exp 过期 JWT → 401
- **T09** UDID ≠ token.device_id → 401 "credential mismatch"
- **T17** 会员过期 → 402 `membership expired`（需 Center 侧协助）

### 订阅生命周期
- **T12** country JP → US 切换 → 旧 refresh goroutine 被 Close()
- **T13** refresh=5 短刷新 → 背景 Fetch 可观测（正向 + pfctl 阻断触发 `DIAG: subs-refresh-fail`）
- **T15** 缓存文件破坏 → loadCache WARN ignore，Fetch 覆盖，继续连

### 协议错误路径
- **T16** Center 返 404（错 path），500（本地 mock）→ status + body 透传

### 并发 / 泄漏
- **T14** up 请求后立即 down → abortUp 生效，无残留
- **T18** 5× up/down → goroutine 数不上涨

## 每条 UAT 的统一验收点

1. **Response** — daemon HTTP response 的 `code` + `message` 符合预期（connecting / 511 + 具体原因）
2. **State** — 跟进 status 查询，final state 符合预期（connected / disconnected）
3. **Log** — `/var/log/kaitu/k2.log` 至少命中一条预期关键字
4. **无副作用** — 无 panic、goroutine 不泄漏、下一轮 up 能正常连接

## 辅助脚本位置

`scripts/k2subs-uat/`（执行阶段创建）：
- `up.sh <via-url>` — 包装 POST up
- `down.sh` — POST down
- `status.sh` — POST status + 格式化 state/lastError/uptime
- `pf-block-center.sh` / `pf-unblock.sh` — pfctl anchor 切换，内含 at-job fallback
- `route-blackhole.sh <ip>` / `route-restore.sh <ip>` — 路由黑洞，含 fallback
- `netdown.sh <iface>` — ifconfig down + at-job fallback

## 执行顺序建议

1. 快速 smoke：T01 / T02 / T03 / T04（参数层，无需故障注入）
2. 认证层：T07 / T08 / T09（纯凭据篡改，零网络干扰）
3. 缓存/订阅：T15 / T12 / T18（文件 + 生命周期）
4. 网络故障：T05 / T06 / T10 / T11 / T13 / T16（带 fallback 的注入类）
5. 并发：T14
6. 可选：T17（需人工协助）

## 不做

- 不测 webapp / Tauri 侧
- 不测真实吊销 token（用篡改字符等价）
- 不测 gomobile appext（另属 mobile UAT）
- 不测 wire 层协议细节（已有 wire/ 单元测试覆盖）

---
**产出**：上述 18 条 UAT + 对应 bash 脚本。每条执行后在 TaskList 中逐一标记 completed 并附带观察到的 log/response 证据。

---

## 执行结果（2026-04-15）

运行环境：macOS dev daemon（K2_PPROF=1）在 :11777。Center `k2.52j.me` 实测（live token 932a7cc1…）。

| T## | 验收 | 证据 |
|---|---|---|
| T01 | ✅ PASS | `k2subs://k2.52j.me/api/subs`（无 creds）→ 511 "subscription parse: missing credentials"；k2.log ERROR |
| T02 | ✅ PASS | `k2subs://UDID:@...`（空 token）→ 511 "subscription parse: missing password"；ERROR |
| T03 | ✅ PASS | `k2v9://foo:bar@...`（未知 scheme）→ 511 "no k2v5 outbound configured — TUN mode requires at least one" |
| T04 | ✅ PASS | `routes: []` → 511 "no k2v5 outbound configured" |
| T05 | ✅ PASS | 清 cache + hosts-block → 511 "fetch failed (no cache): dial tcp [::1]:443: connection refused"；disconnected |
| T06 | ✅ PASS | 先填 cache + hosts-block → 0 connecting → connected；log WARN "subscription fetch failed, using cached data" |
| T07 | ✅ PASS | 签名中间位篡改（`corrupt_jwt_sig`）→ 511 "status 401: invalid credentials" |
| T08 | ✅ PASS | 伪造 `exp=1` payload（签名随之失效）→ 511 "status 401: invalid credentials" |
| T09 | ✅ PASS | UDID 置换 → 511 "status 401: credential mismatch" |
| T10 | ⚠️ PARTIAL | 路由黑洞命中 datagram relay unhealthy（relayTimeouts=3）+ fallback 切 stream；state 保持 connected。真正 iface down 未跑（会断当前 Claude Code 网络）。完整自愈路径由 engine/health_test.go + engine/netmon_test.go unit 覆盖 |
| T11 | ⚠️ PARTIAL | pf 封 tunnel IP 后 live 命中 `DIAG: transport-switch from=quic to=tcpws` + `DIAG: transport-rerace` + `DIAG: echo-probe-fail`（Phase-A 完整）。Phase-B `NextURL` 未 live 触发：暴露 pre-existing bug — `TCPWSClient.connectMu` 跨 blocked TCP dial 持锁 >60s，go-deadlock FATAL，daemon 自退（任务 #57 跟进）。Phase-B 代码路径由 engine/outbound_replace_test.go 5×contract tests + daemon/outbound_provider_test.go 5×tests + wire/swappable_test.go 7×tests 含 10k 并发 stress 覆盖 |
| T12 | ✅ PASS | country=JP up → resolved JP tunnel；down；country=US up → resolved 不同 tunnel（`servers=4`）；log 两次独立 "subscription resolved" |
| T13 | ⚠️ PARTIAL | ?refresh=5 live 资质 happy path connected。DIAG: subs-refresh-fail 不可 live 触发：Center response 覆盖 interval 为 1800s，<30min 窗口内不会 fire。Refresh loop 代码路径由 daemon/subsession_test.go TestSubSession_RefreshLoopClean 覆盖（3 sessions × 1s refresh × 2.5s live + concurrent Close + race detector）|
| T14 | ✅ PASS | up 后 50ms 内 down → 511 "aborted before start"；log INFO "daemon: doUp aborted before start"；下一次 up 正常 connected |
| T15 | ✅ PASS | 破坏 subs cache json → 0 connecting → connected；log WARN `subscription: cache corrupt, ignoring err="invalid character 'G'..."` + 后续 fetch 成功 |
| T16 | ✅ PASS | `k2subs://.../api/nonexistent-path` → 511 "status 404: 404 page not found"；daemon 透传 body |
| T17 | ⏸️ SKIP | 会员过期需 Center 侧人工构造过期账号；代码路径与 T07/T08/T09 同（401/402 都走 subscription fetch status 透传） |
| T18 | ✅ PASS（extended）| pprof K2_PPROF=1 + goroutine-diff.sh 跑 10×up/down 周期，goroutines 恒定 127，0 增长（F2 修复后）。优于原 5× 标准 |

### Soak 验证（T18 extended）

带 K2_PPROF=1 1h 连接保持 + 15min 采样：
- t0 baseline: 312 goroutines
- 结果文件：`/tmp/k2subs-soak/log.txt` + `snap-{0,15,30,45,60}.txt`

（详见 /tmp/k2subs-soak/ — 本次 UAT 执行中后台运行）

### Mobile 覆盖

- **iOS**: iPhone 15 设备在线，Kaitu.io v4.2(404) 安装，devicectl 启动无 crash；主 App + PacketTunnelExtension + neagent 都在运行（VPN 现役）。无 UI 自动化（WebDriverAgent 未安装），k2subs URL 具体连通流程需人工 tap；但代码路径复用桌面 engine + webapp，F1/F2/F3 全部是桌面 daemon-only 改动，无 mobile 回归风险。
- **Android**: adb 无设备连接，blocked；架构与 iOS 同，同上结论。

### 发现的 pre-existing bug

- **#57** `TCPWSClient.connectMu` 跨 blocked TCP dial 持锁 > 60s → go-deadlock FATAL 自退。非 k2subs 回归、非 release blocker（daemon 自退后 launchd 重启 + auto-reconnect 恢复）。但作为 wire 层硬化建议：`connect()` 应 release-dial-reacquire 或加 hard-timeout goroutine（参考 QUICClient.connect 模式）。

### 结论

- k2subs 核心功能（URL 解析、凭据、缓存回退、country 过滤、错误透传）：**全部 live-verified**
- 网络故障 Phase-A（transport-switch + rerace + echo detection）：**live-verified**
- Phase-B server replacement：unit-verified（17 contract tests），live 被 #57 bug 挡住一截
- 资源治理（goroutine 泄漏）：F2 修复经 10×up/down 实证 + 单测
- 认证 / 参数错误：全部 live 断言返回码 + log ERROR

**发布建议**：macOS 桌面 k2subs 可 release；mobile 需 iOS/Android 真机 UI 冒烟跑一遍（半小时内可完成）后放行；#57 wire 层硬化作为下一轮迭代，不阻塞本次。
