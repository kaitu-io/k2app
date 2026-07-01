# 专属节点流量上报合并进 k2s（Option D）Design

> ⚠️ **已被取代 (SUPERSEDED 2026-06-15)** —— 见 `docs/superpowers/plans/2026-06-15-private-node-nic-self-metering.md`（Part 2）。计量已从 k2s 进程**搬回 sidecar**，且改为读**宿主机 NIC 字节**（运营商真实计费数，provider 无关）而非 k2s 应用层字节。`docker-compose.private.yml` override 与 k2s 的三件套 env（`K2_USAGE_REPORT_URL`/`K2_NODE_IPV4`/k2s 侧 `K2_NODE_SECRET`）**已移除**；专属节点仅靠 `.env` 的 `K2_PRIVATE_CLAIM` 区分，sidecar 自己上报到 `/slave/usage`。k2s 代码未改（其 reporter 拿不到 env 即不启动）。本文以下内容仅作历史决策记录。

> 决策日期 2026-06-12。本文是**架构反转的决策记录**：把 Plan 3-k2s Phase B 原本放在 **sidecar** 的「用量上报心跳」**合并进 k2s 进程内**。supersede 了 `2026-06-11-private-node-plan3-k2s-metering.md` 的 Phase B（sidecar 心跳）一节。

## 1. 背景与问题

专属节点的成本闸门是「Center 判 95%/100% → 节点断新连接」。这条闭环需要三件事接力：

| 角色 | 谁做 | 现状 |
|---|---|---|
| ① 数字节（节点级累计 rx/tx） | k2s `ProxyHandler` | ✅ Phase A 已建（`TrafficBytes()`） |
| ② 判 95%/100% + 给 verdict | Center `POST /slave/usage` | ✅ Plan 3-Center 已建 |
| ③ 按 verdict 断新连接 | k2s `accepting` 闸门 | ✅ Phase A 已建（`SetAccepting()`） |
| ④ **把①搬给②、把②的裁决搬给③** | ← **本设计** | ❌ 缺 |

**①和③本就在 k2s。** Phase B 初版把 ④ 这条循环放在 **sidecar**（持密钥、负责 Center 注册的边车进程），于是为了让循环够到①的数据、又能拨动③的闸门，被迫在 k2s↔sidecar 两个独立容器之间架了**两条 HTTP 桥**（k2s 暴露 loopback usage API 供 sidecar 轮询；sidecar 再 POST verdict 回 k2s）。

这两条桥引出一连串本不必要的复杂度：跨容器 `127.0.0.1` 够不到（独立 netns）、我们刻意建的 `isLoopback` 守卫反而挡路、为绕开它要在 host networking（丢端口跳跃抗封）/ 放宽守卫 / Unix socket 之间做取舍。**根因：循环被放错了进程——它的数据源和执行器都在 k2s。**

## 2. 决策：把循环搬回 k2s（Option D）

k2s 自己跑这条循环：读自己的计数器 → POST Center → 把裁决作用到自己的 `accepting` 闸门 + epoch 清零（全是**进程内方法调用**）。sidecar 不再参与计量（仍负责节点注册 + claim 回传，不变）。

```
合并前：数据 k2s→sidecar(桥1)；裁决 sidecar→k2s(桥2)   ← 两次跨容器 HTTP
合并后：数 = r.ctl.TrafficBytes()；断 = r.ctl.SetAccepting()；清零 = r.ctl.ResetTraffic()  ← 进程内
        唯一的网络调用 = k2s → Center POST /slave/usage
```

**消除的东西**：跨容器 IPC、loopback 守卫矛盾、host-networking vs 端口跳跃取舍、sidecar 心跳代码、loopback usage API 的两个 mutating 端点。

**10/10 的关键利好**：verdict/reset 从「跨进程 HTTP（可失败）」变成「进程内调用（不可失败）」。Phase B sidecar 版 review 抓到的「`/reset` 失败却仍 adopt epoch → 下轮拿旧计数器误触发断流」那个 bug **在合并架构里根本不存在**（`ResetTraffic()` 是 in-process store，没有失败路径）。状态机更小、更纯，单测可注入 mock 接口全覆盖。

## 3. 唯一的实质代价：k2s 在专属节点上持有 Center 节点密钥

原拆分**唯一**有分量的理由是密钥隔离：k2s 直面公网，别让它拿 `K2_NODE_SECRET`（认证到 Center 的凭证）。合并后，专属节点的 k2s 需要这把密钥来做 Basic auth。诚实评估：

- **削弱有限**：专属节点是**单租户 VPS**。密钥本来就明文在该机 `.env` 里，sidecar 也持着。能远程读 k2s 内存的攻击者基本也能读磁盘。挡住的只是「有 k2s 内存泄露漏洞、但拿不到磁盘」这一窄场景。
- **危害封顶**：单节点凭证，Center 按节点隔离——最多伪造**这一台**的用量 / 重注册**这一台**，碰不到别的节点或用户。且攻击者已 RCE 该机=已控制它。
- **共享池零影响**：靠「有配置才启用」门——只有专属节点的 k2s 拿到 `K2_USAGE_REPORT_URL`+`K2_NODE_SECRET` 才上报；共享池 k2s 拿不到 → 依旧哑、不持密钥、不联系 Center。

结论：为这条窄纵深防御去维护跨容器 IPC 一大坨复杂度不划算。**接受 k2s 在专属节点持密钥。** 这是本次刻意反转的设计决定。

## 4. 架构（遵守 k2 宪法）

### 4.1 启用门（不变式：共享池零行为变化）
k2s 仅当 `UsageReportURL != "" && NodeSecret != ""` 时启动 reporter goroutine。两者皆来自 env，默认空。共享池节点不设这些 env → reporter 不启动 → 与今天逐字节相同。

### 4.2 ServerConfig 新增（config.go）
```go
UsageReportURL string `yaml:"usage_report_url"` // Center 基址，空=禁用上报。env K2_USAGE_REPORT_URL 覆盖
NodeSecret     string `yaml:"-"`                // env K2_NODE_SECRET。机密，永不入 yaml
NodeIPv4       string `yaml:"-"`                // env K2_NODE_IPV4，可选；空则 ProbePublicIP 兜底
```
`yaml:"-"` 保证密钥/身份永不被序列化进任何配置文件。仅从 env 读（沿用 `K2_USAGE_API_LISTEN` 的 `os.LookupEnv` 范式）。

### 4.3 Reporter（新文件 server/usage_reporter.go）
**Layer Boundary 宪法**：reporter 不依赖 concrete `*ProxyHandler`，而依赖本包内定义的小接口（`ProxyHandler` 自然满足）：
```go
type trafficController interface {
    TrafficBytes() (rx, tx int64)
    ResetTraffic()
    SetAccepting(bool)
}
```
**Encapsulation 宪法**：reporter 只调 `ProxyHandler` 的方法，绝不碰它的 atomic 字段。
**Concurrency 宪法**：reporter 单 goroutine 独占其循环状态（`epochID/seq/lastCeiling`），**无需任何 mutex**；唯一 I/O（Center POST）在任何锁外（本就无锁）。

循环（`runOnce(ctx) (sleep, err)`，`Run` 用 `safego.Go` 启动并 `ctx` 取消）：
1. `rx,tx := ctl.TrafficBytes(); total := rx+tx`
2. POST `{centerURL}/slave/usage`，Basic auth `base64(ipv4:secret)`，**context-aware**（`http.NewRequestWithContext`），body `{epoch_id, cumulative_bytes: total, seq, ts}`。解 Center 信封 `{code,message,data}`，`code==0` 取 data。
3. **失败（离线兜底）**：保留上次 verdict；若 `lastCeiling>0 && total>=lastCeiling` → `ctl.SetAccepting(false)`（离线也能在 100% 硬切）。**不动 epoch**。回短退避 sleep。
4. **成功**：
   - `resp.EpochID != epochID` → `ctl.ResetTraffic()`（进程内，不会失败）；`epochID = resp.EpochID`。
   - `resp.EpochHardCeilingBytes > 0` → `lastCeiling = resp.EpochHardCeilingBytes`。
   - `ctl.SetAccepting(resp.Verdict == "serve")`。
   - `seq++`；`sleep = next_report_interval`（0/缺 → 60s）。
5. **机密卫生**：`secret`、Basic header、含凭证的 URL **永不入日志**。仅记 epoch/total/verdict。仅在 verdict 变化或出错时 log（不 1Hz 刷）。

### 4.4 Center 契约（api/slave_api_usage.go，零改动）
请求 `{epoch_id,cumulative_bytes,seq,ts}`；响应 `{verdict("serve"/"stop"),epoch_id,quota_total,quota_used,epoch_hard_ceiling_bytes,next_report_interval}`。k2s 改由自己 POST（取代 sidecar），Basic auth 同样是 `ipv4:secret`，Center 按 `SlaveNode.Ipv4` 查节点——**故 reporter 的 ipv4 必须 = 注册时的节点公网 IPv4**（优先 `K2_NODE_IPV4` 显式注入，兜底 `ProbePublicIP`，与 sidecar 注册同源）。

### 4.5 收敛 Phase A loopback API（usage_api.go）
外部轮询者消失。删 `POST /reset` + `POST /verdict`（无调用方）。**保留 `GET /usage`**（loopback-only，只读）作运维/ smoke 观测（`curl 127.0.0.1:9099/usage` 看实时计数）。`isLoopback` 守卫保留。

### 4.6 Run() 接线（server.go ~396 后）
```go
if s.cfg.UsageReportURL != "" && s.cfg.NodeSecret != "" {
    ipv4 := s.cfg.NodeIPv4
    if ipv4 == "" { ipv4 = wire.ProbePublicIP("https://api4.ipify.org") }
    if ipv4 != "" {
        rep := newUsageReporter(s.handler, s.cfg.UsageReportURL, ipv4, s.cfg.NodeSecret)
        safego.Go(func() { rep.Run(ctx) })
    } else {
        slog.Warn("usage reporter disabled: node IPv4 undetermined")
    }
}
```

## 5. 测试策略（test = gate）

| 层 | 测试 | gate 内容 |
|---|---|---|
| reporter 单测（mock Center httptest + mock `trafficController`） | serve→stop 调 `SetAccepting(false)`；stop→serve 调 `SetAccepting(true)`；epoch 变 → `ResetTraffic()` + adopt；report body 形状（epoch/total/seq 单调/ts，断言反序列化 map）；**离线 ≥ceiling → `SetAccepting(false)`**；离线 <ceiling 不切；**URL 空 → reporter 不启动/no-op**；**secret 不入日志**（捕获 slog 断言） | 断流状态机全覆盖；机密卫生；启用门 |
| config 单测 | env `K2_USAGE_REPORT_URL`/`K2_NODE_SECRET`/`K2_NODE_IPV4` → 字段；缺 → 禁用；`yaml:"-"` 不序列化密钥 | 配置门 |
| 集成（真 `ProxyHandler` + mock Center） | 经 `pipe` 搬已知字节 → reporter 读真计数器 → POST mock Center → 翻真 `accepting` 闸门 | 端到端进程内闭环 |
| Phase A 收敛回归 | `GET /usage` 仍对；`/reset`+`/verdict` 已删；非 loopback 仍 403 | 不回归 |
| 全量 | `cd k2 && go build ./... && go vet ./...`；`go test ./server/ ./config/`（-race 谨慎，300s+） | 无回归 |

桌面可达 ~9.5/10。**真机断流 smoke**（真 VPS 跑流量到 95% 看断、月度 epoch 清零、续费恢复）= 发布闸门，按约定与 keystone 三连一起批量验。

## 6. 对已建工作的影响
- **撤回 sidecar 心跳**：删 `docker/sidecar/sidecar/usage_heartbeat.go`+test、`config.go` 的 `UsageAPIURL`、`entrypoint.sh` 的 `usage_api_url`、`main.go` 的接线（commits `dcdbdabe`/`9234769c` 的内容）。逻辑/状态机/测试平移进 k2，非白做。
- **保留**：claim 穿线（`35617bd6`）、3 个 provision-job MCP 工具（`fde87e40`）—— 都与计量无关。
- **k2 Phase A**：计数器+闸门留用；loopback API 收敛（删 2 个 mutating 端点）。
- **docker-compose.private.yml**：大幅简化——给 k2v5 容器传 `K2_USAGE_REPORT_URL`+`K2_NODE_SECRET`+`K2_NODE_IPV4`（+已有 traffic 配额 env），**无任何网络体操**（bridge 不变、端口跳跃保留、Docker 端口映射不变）。

## 7. 部署
- k2s 二进制随版本重编 + 节点滚动升级（沿用现有 k2s release/upgrade 链）。
- 专属节点 `.env` 需含 `K2_USAGE_REPORT_URL`（Center 基址）、`K2_NODE_SECRET`、`K2_NODE_IPV4`（开通 agent 注入，它知道刚建实例的公网 IP）。
- 共享池节点：不设这些 env → 零变化。
- k2 子模块在 k2 仓内 commit，**parent 的 submodule 指针保持 unstaged**。
