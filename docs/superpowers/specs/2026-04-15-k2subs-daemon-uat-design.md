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
