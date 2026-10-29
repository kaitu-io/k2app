# 可续传 TCP 代理流（Resumable TCP Streams）—— 设计

**日期**：2026-10-28
**状态**：设计稿，未实现
**范围**：k2 submodule —— `wire/`（新流类型、续传连接、会话表、能力协商）+ `server/`（计量键透传）+ `engine/`（观测字段）。`core/` 与 `server/handler.go` 的 `pipe()` 不改。
**触发工单**：k2app #3951 / #3972（用户 6365，Android 0.4.13，联通山东移动网络，"经常连不上 chatbox"）

---

## 1. 为什么做

### 1.1 现象与根因（工单分析，根因置信度 8/10）

两份客户端日志共 186 次健康劣化（客户端日志 + 节点 52.221.79.146 的 k2s.log 交叉验证；"为何读数恰好是 ~0.7"的 k2cc 算术路径尚未用单测复现，故不是 10/10）：

- 157 次（84%）在 ±20s 内**没有任何独立的路径故障证据**（echo 全通、拨号成功率 95–98%、RTT 不升、服务端照常收到请求且自测下行丢包 0）—— 健康判定在低流量 QUIC 上误报 critical。
- 约 16% 是用户手机网络真实的短暂中断（10/27 08:12：直连国内 DNS 同时超时，服务端 25s 收不到包后同一毫秒涌入一批请求）。

两类都会触发 wire 层的「换连接」动作，而**换连接会杀掉所有在途代理连接**。用户看到的 `Failed to fetch` 来自这里，不是来自线路本身。

即使误报比例有偏差，本设计的必要性也不依赖它：真实的路径中断（第二类，以及 #3747/#3757 的运营商形态）同样会杀掉在途连接。

### 1.2 结构性缺陷

每条代理 TCP 连接绑死在一条具体的传输流上：

```
app ⇄ gVisor ⇄ core.pipe() ⇄ [QUIC stream | smux stream] ⇄ server.pipe() ⇄ net.Dial(upstream)
                               ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
                               这条流死 = 整条逻辑连接死
```

杀死在途流的三个动作（均在 `wire/transport.go`）：

1. `runProbeOnce` —— fallback 期间每 30s 一次，先 `qc.resetConnection()`，关掉仍承载旧流的 QUIC 连接；
2. 健康 critical 重连 —— `ResetConnections()`，两条传输一起断；
3. re-race —— `ReplacePrimary` 关闭旧 primary。

此外还有不可控的：真实路径黑洞（QUIC idle 30s 后才判死）、运营商丢 bulk UDP（#3747/#3757 形态）、手机网络切换（Wi-Fi ↔ 蜂窝，源 IP 变）。

### 1.3 目标

**把「逻辑连接」与「承载它的传输」解耦**：每条代理 TCP 连接有一个与传输无关的 conn ID，传输层（QUIC / TCP-WS，或同类传输的新连接）可以随时更换，逻辑连接在同一节点上无损续传，应用和目标服务器都感知不到。

---

## 2. 目标 / 非目标 / 反目标

**目标**
- G1 同节点、任意传输组合（QUIC→QUIC 新连接、QUIC→TCP-WS、TCP-WS→QUIC、TCP-WS→TCP-WS）之间续传在途 TCP 代理连接，字节**恰好一次、按序**。
- G2 覆盖第 1.2 节列出的全部断开来源，包括源 IP 变化。
- G3 与老客户端、老服务端双向兼容；任一端不支持时行为与今天完全一致。
- G4 有总开关，可在两端分别关闭。

**非目标**
- N1 跨节点续传（上游连接只存在于原节点；换节点 = 今天的行为）。
- N2 UDP / stream-UDP（DNS）续传。UDP 本身可丢，内层协议自愈；以后需要再单独设计。
- N3 k2s 进程重启 / 升级后的续传（会话表在内存里）。
- N4 修复健康误判本身（k2cc 低样本丢包、echo 把关）—— 另立任务，与本设计正交，**两者都要做**。

**反目标（做成这样就算失败，哪怕"能用"）**
- A1 续传引入重复或乱序字节 —— 上层是 TLS，一个字节错就是连接级失败，比断开更糟。
- A2 被吊销/过期的设备借续传继续用流量（绕过鉴权门）。
- A3 iOS NE 因续传缓冲触发 jetsam。
- A4 正常（不切换）路径的吞吐、延迟、CPU 有可测的退化。
- A5 conn ID 可被第三方猜到或重放来劫持别人的连接。

---

## 3. 方案取舍（已与 David 确认选 B）

| | A. 退役而不关闭 | **B. 可续传逻辑连接（本设计）** | C. 只修误判 |
|---|---|---|---|
| 做法 | 换连接时旧 QUIC 不主动关，旧流自然跑完；探测用独立连接 | conn ID + 双向字节序号 + 未确认缓冲 + 服务端宽限期，断后在任意传输上续接 | k2cc 低样本不采信、critical 需 echo 同意 |
| 改动面 | 仅客户端 wire | 两端 wire + 协议 | 客户端 engine/wire |
| 解决误报杀流 | 是 | 是 | 是（减少触发） |
| 解决真实路径故障 / IP 变化 | **否** | **是** | 否 |
| 代价 | 小 | 大（周级） | 小 |

选 B 的理由：只有 B 兑现"tunnel 层可以随意切换"——A 和 C 只是让切换少发生或少伤害，路径真坏时仍断。C 作为独立任务并行推进。

---

## 4. 总体结构

续传逻辑放在 **wire 层**，以一个 `net.Conn` 实现（下称 `rconn`）出现在两端：

```
client:  core.pipe() ⇄ rconn ──(当前承载流, 可替换)──┐
                         │                          │ QUIC stream 或 smux stream
                         └─ 续接时向 TransportManager │
                            要一条新流 (任意通道)       ▼
server:  server.pipe() ⇄ rconn ⇄ sessionTable[connID] ⇄ (承载流, 可替换)
```

- **客户端**：`TransportManager.DialTCP` 在服务端已声明支持、且预算允许时，返回 `rconn` 而不是裸流。`core/tunnel.go` 拿到的仍是 `net.Conn`，零改动。
- **服务端**：两种传输的 `handleStream`（`wire/quic.go` / `wire/tcpws.go` 的 streamType switch）分发新流类型。`0x05` 走 `admitTCP`（同一个 `admissionPolicy.admit`），建立 `rconn` 并登记进**节点级、两种 listener 共享**的会话表，照常 `sendTCP` 进 `AcceptTCP` 队列。`ProxyHandler.HandleTCP` 对着 `rconn` 写响应、`pipe()`，零改动。上游 `net.Dial` 连接的生命周期从此跟随 `rconn`，不再跟随承载流。`0x06` 走新的 `admitResume`（同一判定），它不产生 `sendTCP`，只把新承载流交给已有会话；`TestAdmissionGate_EgressSinksAreGated` 的白名单要显式覆盖它。
- 层次：wire 内部新增，不向上暴露新接口；`TransportManager` 是 wire 自己的类型，`rconn` 回调它取新流不违反层边界规则。engine 不改（除观测字段）。

---

## 5. 协议

### 5.1 能力协商

服务端在 metadata 响应的 kv（`[0x00][MetadataFrame kv]`，老客户端会安全丢弃）里加入：

- `resume=1` —— 本连接可用续传
- `resume_grace_s=<n>` —— 服务端宽限期（客户端据此定续接截止时间）

**只对 verdict OK 且服务端 verdict udid 非空的连接声明**。注意两者不等价：无 validator 的 permissive 路径会返回 `authed=true, udid=""`（`wire/metadata_auth.go` 的 `validate == nil` 分支），这种连接**不声明**。未鉴权连接同样不声明，走今天的 `0x01`。理由见 §8。

客户端只在**当前这条传输连接**收到 `resume=1` 后才对它开 `0x05`。续接（`0x06`）同样要求承载它的那条传输连接已声明 `resume=1`。

### 5.2 新流类型

沿用现有流头外壳：QUIC 为 `[H3 frame type 0x4B32][StreamType][addr_len][addr]`，TCP-WS 不写 `0x4B32` 标记、只有 `[StreamType][addr_len][addr]`（见 `metadata_client.go` 的 `writesH3FrameType`）。两种传输共用 `WriteStreamHeader`/`ReadStreamHeader`，新流类型的差异与现有 `0x01` 完全相同。

| 值 | 名称 | 方向 | 头部之后的内容 |
|---|---|---|---|
| `0x05` | `StreamTypeTCPResumable` | C→S 开新连接 | `addr` = 目标；随后 `[connID:16B]`，之后进入帧模式 |
| `0x06` | `StreamTypeTCPResume` | C→S 续接或放弃已有连接 | `addr` 为空；随后 `[connID:16B][attemptSeq:varint][flags:1B][clientRecvOffset:varint]` |

- `attemptSeq`：同一 rconn 每次续接尝试单调递增（从 1 起）。服务端只接受**严格大于**已见最大值的 seq（见 §6.4）。
- `flags`：bit0 = `ABORT`。客户端放弃一个 detached 连接（截止时间到、上层中止）时，尽力在任一活连接上发一个 ABORT 的 `0x06`，服务端立即释放会话、关上游，不回复帧模式（见 §6.5）。

`0x06` 的服务端回复（帧模式之前，裸字节）：
- 成功：`[0x00][serverRecvOffset:varint]`，之后进入帧模式
- 失败：沿用 `WriteStreamResponse` 的错误格式：
  - `404` 会话不存在 / 已过期 → 客户端放弃续传，rconn closed（对应用表现为 reset）。**例外**：rconn 处于 linger（§5.4）时，404 表示对端已正常释放，按成功结束处理。
  - `403` udid 不匹配或会话已被吊销 → 放弃，closed。
  - `409` 本次 `attemptSeq` 已过期（有更新的尝试）→ **忽略这次尝试**，不是致命错误；rconn 继续等更新那次的结果。
- 承载 `0x06` 的连接被以吊销码关闭（QUIC `CloseWithError` 码 `0x03`；TCP-WS 的 kill 只是普通 `sess.Close()`，`wire/tcpws.go` ~917，**没有**可区分的关闭码——TCP-WS 上客户端会照常尝试续接并拿到 `403`，这是预期行为）→ 客户端不再续接该连接上的任何 rconn。

`connID`：客户端 `crypto/rand` 生成 128 bit。会话表以 `(udid, connID)` 为键。`0x05` 若撞上已存在的键回 `409`；per-request fallback 的每次重试**必须换新 connID**。

### 5.3 帧格式（帧模式，两个方向对称）

```
DATA : [0x00][offset:varint][len:varint][payload]   len ≤ maxDataFrame (16 KiB)；offset = payload 首字节的流内偏移
ACK  : [0x01][recvOffset:varint]                    累计：我已按序收进 rconn 接收缓冲的对方 [0, recvOffset)
FIN  : [0x02][finalOffset:varint]                   我方写方向结束（半关闭），finalOffset = 我方总发送字节数
RST  : [0x03][code:varint]                          立即终止整条逻辑连接，不可续传
WINDOW: [0x04][maxOffset:varint]                   授信：对方可以发到偏移 maxOffset 为止（见下"流控"）
```

未知帧类型 → RST（保留以后扩展只能靠能力协商，不靠"忽略未知帧"）。

- 字节偏移按**每方向**从 0 计数，只计 DATA payload。DATA **显式携带 offset**，不依赖"每次附着的起点"这类隐式约定：收方对每个 DATA 帧，丢弃 `< recvOffset` 的部分，`> recvOffset`（出现空洞）视为协议错误 → RST。这是"恰好一次、按序"的唯一机制，**必须有单测和 fuzz 锁住**。
- 续接握手时双方声明的偏移必须校验：对方声明的 recvOffset 小于它此前已 ACK 过的值，或大于我方实际发送过的总字节数 → RST。
- 服务端写给客户端的 stream response（`WriteStreamResponse`）就是偏移 0 起的普通 DATA 字节；客户端 `DialTCP` 照旧在返回前读完它，**并立即 ACK**（不等 50 ms 计时器，服务端据此判断 opening 是否完成，见 §6.5）。earlyData 是客户端偏移 0 起的第一个 DATA 帧。
- 收到 RST 时，RST 之前已按序收到的 DATA 照常交给上层，读完后上层才看到错误。否则服务端 `HandleTCP` 的 `WriteStreamResponse(错误) → defer Close()`（=DATA + RST）会把 403/502 变成传输错误，破坏 proxy-layer 错误分类和 per-request fallback 的判断。
- ACK 发送时机：自上次 ACK 起新收进 ≥ `ackEvery`（默认 64 KiB）**或** 距上次 ACK ≥ 50 ms 且有未确认数据（response 字节例外，立即 ACK）。ACK 可与反向 DATA 帧交错。
- 释放条件：我方 FIN 被对端 ACK 到 finalOffset **且** 已收到对端 FIN 并 ACK 过它；RST 无需确认，发出或收到即释放。

**收发解耦与流控：接收方授信**（反目标 A3 / A4 的关键）：

- 新增第五种帧 `WINDOW : [0x04][maxOffset:varint]`：接收方声明"你可以发到偏移 maxOffset 为止"。发送方未获授信的字节**不得发出**，上层 `Write` 在授信耗尽时阻塞。这与 QUIC 的 MAX_STREAM_DATA、HTTP/2 的 WINDOW_UPDATE 是同一个机制。
- **接收方只按自己已预留的内存授信**：授出 N 字节之前，先从全局字节信号量里拿到 N。所以接收方收到的任何 DATA 都有地方放，**解帧 goroutine 永远不需要停止读承载流**，ACK/FIN/RST/WINDOW 永远能被看见。
- 为什么不能用"信号量满时接收方停读承载流"来背压：控制帧和 DATA 在同一条字节流里，停读就看不到排在后面的 ACK。具体死锁：移动端几条慢读的下载占满信号量；HTTP 上传连接 Y 的应用阻塞在 `Write` 等 ACK、写完之前不 `Read`；服务端发来 Y 的早期响应 DATA，后面跟着 ACK；Y 的接收方放不下这段 DATA 就停读，再也看不到 ACK，`Write` 永远不返回，Y 也永远不释放它占的份额。今天不会发生，因为 QUIC 的流控按方向独立、ACK 在传输层。
- **ACK 的含义是"收进 rconn 的接收缓冲"**，不是"被上层读走"。上层读走数据后，接收方释放信号量、再授新的信用（WINDOW），每读走 ≥ 窗口的 1/4 授一次。
- 窗口**由接收方按自己的平台决定**，不是发送方：每条 rconn 的接收窗口上限 = 移动 2 MiB、桌面/k2r 8 MiB、服务端 8 MiB（与今天承载传输的单流窗口一致，`wire/quic_limits_*.go`，所以单连接吞吐上限与今天相同，A4）。初始授信 = 64 KiB（随 `0x05` / response 一起声明），之后随读取增长到上限，信号量紧张时少授。
- 发送方保留的重传缓冲 ≤ 对端已授信但未 ACK 的部分，也从本端信号量取；取不到时 `Write` 阻塞（只影响发送，不影响解帧）。
- 背压的方向因此只有一个：**不读的连接停止授信，对端停止发送**；别的连接不受牵连。

### 5.4 正常关闭语义映射

两端 `pipe()` 都不改，所以 rconn 只能从**调用顺序**推断意图，不能指望上层显式告诉它"这是 reset"：

- `core/tunnel.go` `pipeDirExit`：干净 EOF → `CloseWrite()`；出错 → 两侧直接 `Close()`。
- `server/handler.go` `pipe()`：每个方向结束都 `CloseWrite()`，两个方向都结束后才 `Close()`。

规则：

| 本端事件 | 发送 | 说明 |
|---|---|---|
| `CloseWrite()` | FIN | 保持半关闭语义 |
| `Close()`，我方 FIN 已发出 **且** 对端 FIN 已收到 | 无，进入 **linger** | 干净结束。上层立即返回，rconn 在后台保留会话，直到我方 FIN 被 ACK（或 linger 上限到，取客户端续接截止时间）再释放。linger 期间承载流断了照常续接，保证尾部字节送达（A1）。core 的 `pipe()` 在两个方向都 EOF 后立刻 `defer remote.Close()`，此时我方 FIN 多半还没被 ACK——没有 linger，几乎每次正常关闭都会被误判为中止 |
| `Close()`，我方 FIN 未发出 **或** 对端 FIN 未收到（含 core 出错路径、pipeWatchdog 强关） | attached：RST，立即释放。detached：发 ABORT 的 `0x06`（§5.2），立即释放 | **中止**。保留今天 `quicStreamConn.Close()` 里 `CancelRead` 的效果：对端写立即失败，服务端立刻关上游，不会把数据读进一个没人取的缓冲 |
| 承载流读写出错、对端流被重置、承载连接关闭 | **什么都不发**，进入 detached | 这是唯一触发续传的情况（opening 阶段除外，见 §6.5） |

已知的语义损失与今天一致：服务端 `pipe()` 在上游 reset 时也只 `CloseWrite()`，所以客户端看到的是 FIN 而不是 RST——今天的 `0x01` 也是如此，本设计不改善也不恶化。

"承载流出错"与"对端主动关闭"在帧层是**可区分的**：后者一定先收到 FIN / RST 帧。这条区分不能靠错误字符串推断（遵守 Error Ownership 规则：由 rconn 自己根据是否收过 FIN/RST 判定）。

---

## 6. 状态机

### 6.1 rconn 状态（两端共用）

```
            open ok
 opening ───────────► attached ◄──────────────┐
    │ fail                │ 承载流出错         │ 续接成功
    ▼                     ▼                    │
  closed            detached ─────────────────┘
                          │ 截止时间到 / 续接被拒 / 本端中止
                          ▼
                       closed
 attached/detached ── 收到或发出 RST ──► closed
 attached ── 双向 FIN 已交换后 Close() ──► linger ── 我方 FIN 被 ACK / linger 上限 ──► closed
                                            └─ 承载流断 ──► detached(仅为送达尾部)
```

- **opening**：`0x05` 已发，等服务端 response。此阶段失败 = 今天的普通拨号失败，原样返回给 `TransportManager`（per-request fallback、data-path distress 记账都不变）。**opening 阶段不续传**，并且两端都按中止清理（§6.5）。
- **attached**：有承载流，正常收发。
- **detached**：无承载流。上层 `Read` 阻塞（先读完已收进接收缓冲的数据），上层 `Write` 写进发送缓冲直到对端授信用完后阻塞。
- **deadline**：上层设置的 `SetDeadline/SetReadDeadline/SetWriteDeadline` 只作用于 rconn 自己的阻塞点，**永远不转发给承载流**。承载流上的 deadline 超时会被误判为承载出错，触发假 detach。

### 6.2 客户端：何时、如何续接

触发（任一）：
1. 承载流读/写返回错误，且未收到过对端 FIN/RST；
2. **承载连接本身关闭**：rconn 监听承载它的那条传输连接的关闭信号（QUIC `conn.Context().Done()`、smux session 的关闭通道）。不挂在 `TransportManager` 的方法上，因为退役路径不全经过它：engine re-race 直接 `SwapTCPWS` 后 `oldTCPWS.Close()`（`engine/engine.go` ~2152），`cmd/k2/service_darwin.go` ~378 直接 `cur.Reset()`。所有退役动作最终都会关闭连接，所以连接级信号能覆盖全部路径，包括以后新加的。触发 2 让续接**主动**发生，不必等旧路径 30s idle 判死——这是"随意切换"在体验上成立的关键。
3. **降级切换驱动迁移**：通道选择器从 quic **降级**到 tcpws 时，把所有承载在 QUIC 上的 rconn 迁到 tcpws（错峰发起，例如每 10 ms 一条，避免同一瞬间开几百条流）。**tcpws→quic 的回切不迁移**：回切是试探性的（trial、`failedTrials`、flap 抑制、`rollbackAt`，`wire/transport.go` ~836-933、~1010-1030），而且 echo 成功就会触发回切；在 #3747 形态（echo 通、bulk 丢）里，每次回切都把正在跑的下载搬到一条没被证明的路径上卡住，trial 失败再搬回来。已在 tcpws 上的连接留在原地，与今天一致；新连接照常走 active channel。
   - 为什么需要：#3747/#3757 的运营商形态里，握手、keepalive、echo 都能通过，只有 bulk 被丢。选择器切到 TCP-WS 后，挂在旧 QUIC 上的大下载要等 fallback 期间 `runProbeOnce` 的下一次 `resetConnection()`（`wire/transport.go` ~1500，每 30 s）才会被触发 2 救回，最长卡 30 s。切换即迁移把这段时间缩到约一个 RTT，代价不高于随后那次 reset 本来就会造成的续接。
   - 为什么不用"未确认字节停滞计时"：大下载里客户端几乎不发数据，客户端侧的计时根本不会启动；服务端又不能主动开流；而且上层读得慢、上游背压时也会没有 ACK，计时会误触发。通道选择器已经汇总了 echo、记分和 data-path 信号，是更可靠的判断者，rconn 不另做通道判断。
   - 接法：`TransportManager` 修改 `selState.active` 的地方不止一处——`evaluateSelector`、`evaluateSelectorLegacy`、`SetFallbackActiveWithReason`、legacy 的 `runProbeOnce`、`Reset`，以及 `ReplacePrimary` 会静默把 active 置回 quic（`wire/transport.go` ~1017）。不在这些地方逐个挂钩，而是在 `selState.active` 的**唯一写入函数**里发一个变更通知（builder 把现有直接赋值收敛到它，并加一个 AST 守卫：`selState.active` 不得在该函数外被赋值）。
   - 迁移器收到通知后**等 250 ms 静默期**再读一次当前 active 和变更方向，只有静默期结束时的净变化是 quic→tcpws 才迁移。原因：`applyReRaceResult`（`engine/engine.go` ~2151）依次 `ReplacePrimary`（active=quic、关旧 QUIC）→ `SwapTCPWS` 并关旧 tcpws → `SetFallbackActive(true)`，中间状态会让 rconn 先续接到即将被关的连接上再迁一次。静默期过后只看最终状态。re-race 关掉的旧连接本身由触发 2 处理，目标是静默期后的 active channel（触发 2 的续接同样等这 250 ms 再选通道）。
4. 承载连接仍活、通道也没被切走时：不迁移。

动作：
- 向 `TransportManager` 请求一条 `0x06` 流，走**当前 active channel**（与新连接的通道选择一致）；失败则按 per-request fallback 尝试另一通道。
- 重试退避：0、250 ms、500 ms、1 s、2 s…（上限 2 s），每次尝试 `attemptSeq+1`。一次尝试超时未回复就发下一次，不必等它失败；更新的尝试自动取代旧的（§6.4）。截止时间：从**最后一次收到服务端任何帧**的时刻起算 `resume_grace_s − 5 s`（默认 55 s）。不从客户端自己发现断开的时刻起算：服务端可能先于客户端发现断开（例如中间设备重置了服务端一侧的 TCP-WS），两端时钟起点不同时，客户端会在自以为有效的窗口里拿到 404。
- 与 `pipeWatchdog` 的关系：watchdog 的 30 s 是从**第一个方向退出**时起算（`core/tunnel.go` `pipeWatchdog`），不是从 detach 起算，只影响已半关闭的连接。半关闭的连接在 detached 期间只能用到 watchdog 剩余的时间，被强关时按上表"中止"处理（ABORT）。双向都开着的连接不受 watchdog 影响，可以用满截止时间。不为此设不变量测试——两个时钟起点不同，这样的测试检查不到真实问题。
- 成功：从 `serverRecvOffset` 起重发本端发送缓冲里的字节，对端同理；回到 attached。
- 截止时间到或收到失败回复：closed，上层 Read/Write 返回错误（等价今天的 reset）。

`TransportManager` 被 `Close()`（engine 停止 / 换节点 `replaceOutboundServer`）时，所有 rconn 立即 closed，不尝试续接。

### 6.3 服务端：detached 期间

- 承载流断开后会话进入 detached，宽限期计时开始（默认 60 s，`K2_RESUME_GRACE_S` 可调）。
- 注意服务端何时**发现**断开：服务端 QUIC 的 MaxIdleTimeout 是 **90 s**（`wire/quic.go` ~260，客户端是 30 s）。路径静默黑洞时，服务端最长 90 s 后才 detach，再加 60 s 宽限，会话最长占用约 150 s。客户端那边早已续接成功（续接一到达，服务端立即切到新承载流，不必等旧的超时，见 §6.4），所以这只影响**客户端已放弃**的会话——由 ABORT（§6.5）提前释放。
- **立即停止读上游**，不预读。数据留在目标服务器到节点之间的 TCP 缓冲里，续接后再读；预读几乎换不来什么（续接后一个 RTT 就能追上），却会让一次共享路径抖动导致的大批同时 detach 把节点内存吃满、背压所有 attached 会话（跨租户吞吐塌陷，违反 A4）。服务端在 detached 期间只持有断开那一刻的未确认在途字节（§7）。
- 宽限期到 → 以 **RST** 关闭上游（`SetLinger(0)`），删除会话。用 RST 而不是 FIN：上传被截断时目标服务器必须看到异常结束，而不是一个看似完整的请求体。（今天 `pipe()` 出错路径给目标的也是 FIN，`server/handler.go` ~873-896；这里是顺手改进，不是回归。）
- 上游在 detached 期间关闭：因为不读上游，服务端要到续接后读上游时才会看到 FIN/RST，按正常顺序送达。

### 6.4 并发续接与陈旧附着

规则：**按 `attemptSeq` 新者胜**。

- 服务端每个会话记录已见的最大 `attemptSeq`。`0x06` 的 seq 严格更大 → 接受：在会话表锁下记下新 seq、把会话切到这条新承载流，旧承载流（无论是已附着的，还是正在握手中的较早尝试）被关闭，之后它的任何帧都丢弃。seq 不大于已见最大值 → `409`。
- 客户端：同一 rconn 可以有多个尝试在途（上一次超时未回复就发下一次）。拿到**最高 seq** 的成功回复即附着，其余尝试的流直接关闭；`409` 只表示"这次已被取代"，忽略。
- 为什么不是"先完成的赢"：尝试 1 已在服务端附着但回复丢了，客户端在另一通道发尝试 2——按"先到先得"它会拿 `409`，客户端只能关掉连接，正好是本设计要避免的结局。反过来如果按"后到覆盖"但不带 seq，一个迟到的尝试 1 会把会话抢回到客户端已经放弃的流上，连接卡死。
- 偏移的一致性：切换承载流时，收方在同一把锁下取快照 recvOffset 并隔离旧承载流，之后旧流上迟到的 DATA 一律丢弃；有了 DATA 显式 offset（§5.3），即使有残留也不会重复交付。
- 客户端发 `0x06` 时取的 `clientRecvOffset` 快照，同样要在同一把锁下**隔离旧承载流**（之后不再在旧流上收 DATA、发 ACK）。否则旧流上又发出一个更大的 ACK，服务端随后看到的声明值会小于已 ACK 的值，§5.3 的偏移校验会把一次合法迁移判成 RST。

### 6.5 中止与清理（不让服务端空占上游）

- **opening 阶段失败**：客户端响应超时、写失败或被 per-request fallback 放弃时，若承载流还活着，先在流内写一个 RST 帧再关闭。服务端规则：**我方 response 字节被客户端 ACK 之前**承载流出错，按中止处理（关上游、删会话），不进入 detached。客户端在读完 response 后立即 ACK（§5.3），所以正常连接很快越过这道线。
  - 为什么必须：#3757 形态里三分之一的拨号超时。若服务端把这些当作 detached，每一个都会继续拨上游、读数据、占 60 s。
- **detached 期间客户端放弃**（截止时间到、上层中止、watchdog 强关）：客户端在任一活连接上尽力发 ABORT 的 `0x06`（不等回复，失败也不重试）。服务端收到后立即释放会话、以 RST 关上游。ABORT 同样受 udid 绑定校验（§8），**不检查 `attemptSeq`**（放弃是终态，任何一次放弃都生效；ABORT 携带的 seq 字段填当前值、服务端忽略）。
- `0x05` 的失败回复：`409`（connID 撞键，128 bit 随机下只可能是 bug）和 `503`（服务端缓冲 / 每 udid 上限满）都**在 wire 层拦截**，客户端立即以 `0x01` 重开同一请求，不把错误交给 engine——否则会被当成 `ServerUnreachable`（503）进入错误分类和 fallback。
- 发不出 ABORT（所有连接都断了）时，服务端靠宽限期兜底。

---

## 7. 缓冲与内存

两种缓冲，各自的上限来源不同：

**发送方的重传缓冲 = 只有真正在途的字节**。ACK 的含义是"收进对端 rconn 的接收缓冲"（§5.3），对端读得慢时，数据堆在**对端的接收缓冲**里（受对端授信封顶），不堆在发送方。所以发送方未确认的字节只剩"已发出、对端还没收到或 ACK 还在路上"的那部分：

> 发送方额外内存 ≈ 本端总发送吞吐 × (RTT + ACK 延迟)

被出口带宽封顶、与连接数无关，也与对端读得快慢无关。例：跑满 1 Gbps 的 k2s，RTT 300 ms + ACK 延迟 50 ms ≈ 125 MB/s × 0.35 s ≈ **44 MB**。客户端只保留上行，手机上行几 Mbps，额外占用 < 1 MB。数据同时在承载传输的发送缓冲和 rconn 重传缓冲里，所以是"在途那一份翻倍"，不是进程内存翻倍。

**大批同时 detach 不放大它**：detached 时服务端不再读上游（§6.3），持有的只是断开那一刻的在途字节，总量仍 ≈ 上式。所以一次共享路径抖动断掉几千个会话，内存不会因此飙升，也不会背压其他 attached 会话。

**接收方的接收缓冲 = 自己授出的信用**。这部分与"慢读连接数"成正比，由全局字节信号量封顶：授信之前先拿信号量（§5.3）。

| | 单 rconn 接收窗口上限（由接收方定） | 每 udid 上限 | 全局信号量（接收缓冲 + 重传缓冲） | 信号量紧张时 |
|---|---|---|---|---|
| 客户端（移动 `ios \|\| android`） | 2 MiB | — | 8 MiB | 少授 / 不再扩大授信；新连接降级为 `0x01` |
| 客户端（桌面） | 8 MiB | — | 64 MiB | 同上 |
| 客户端（k2r 路由器） | 8 MiB | — | `min(64 MiB, 系统内存 × 5%)` | 同上 |
| 服务端（k2s） | 8 MiB | 64 MiB | `min(1 GiB, 启动时 MemTotal × 15%)`，`K2_RESUME_MEM_MB` 可覆盖 | 少授上行信用；新 `0x05` 回 `503` |

- **iOS 的 8 MiB 是真正的硬顶**：接收方授出去的每个字节都已从信号量里预留，所以服务端无论如何发，都放得下；慢读的下载最多占它自己的 2 MiB 窗口。代价是同时有多条慢读大下载时，后来的连接只拿到较小的授信，吞吐下降——这是用内存换的，以 §11 的真机测量为准。
- **保底授信**：每条 rconn 创建时从一个预留池里拿 64 KiB 的初始授信，保证任何连接至少能推进；预留池用完时新连接直接走 `0x01`（它用的是承载传输自己的流控和内存），不会出现"连上了但一个字节都收不到"。
- **不死锁**：解帧 goroutine 从不等信号量（收到的 DATA 都在已授信范围内），控制帧随时可见；只有"扩大授信"和"发送方存重传字节"会等信号量，前者等待只会让对端慢下来，后者只阻塞本连接的上层 `Write`。builder 需要用测试锁住"信号量耗尽时，所有连接的 ACK 照常推进、上传不被下载阻塞"。
- 服务端全局上限**按内存比例取值**，不用固定值。能力在客户端放量之前就会对整个车队开启，其中有小内存的 Lightsail 机器；如果 k2s 因此被 OOM kill，所有会话会一起丢失（N3），比今天更糟。路由器同理。
- 每 udid 字节上限（服务端 64 MiB，k2r 例外见 §8）：只限会话数不够，单台设备几百条连接就能吃满全局。到上限后该 udid 的新授信变小，新 `0x05` 回 `503`。
- 服务端回 `503` 时，客户端对这条连接立即以 `0x01` 重开（§6.5），不计入 data-path distress。
- 缓冲内存按需分配（分块链表，块 16 KiB，复用池），不预分配窗口。
- **数值均为起点**，由 builder 用 nslab 和真机实测后定稿。


---

## 8. 安全与鉴权

- **续接必须经过鉴权门**。`0x06` 在两种传输上都走 `admitTCP` 的同一判定（`admissionPolicy.admit`），并扩展 `TestAdmissionGate_EgressSinksAreGated` 的 AST 扫描覆盖新入口。续接不创造新出网，但它把一条已有出网交给新连接，等价于出网。
- **绑定键是 `authSession.udid`（首次鉴权写入、之后不可变），不是 `ctx.dev`**：今天每个 metadata 流都会无条件 `ctx.dev.set(st.udid)`（`wire/quic.go` handleMetadataStream、`wire/tcpws.go` ~1021），一条连接可以发多个 metadata 流，而 `authSession.markAuthed` 是先到者胜（`wire/session.go`）。用 `ctx.dev` 绑定，等于让同一条连接中途换身份去接别人的会话。（计量用 `ctx.dev` 的既有漂移今天就存在，不在本设计范围，但值得单独开一个问题。）
- **udid 绑定是无条件的，不依赖 enforce 模式**：permissive 模式（生产默认）下 `admissionPolicy.admit` 总是放行（`wire/admission.go` ~119），所以光靠"同一判定"什么也挡不住。真正的防线是 udid 匹配：会话记录创建时承载连接的服务端 verdict udid；`0x06`（含 ABORT）所在连接的 verdict udid 必须非空且相同，否则 `403`。这个检查写在 `admissionPolicy` **之外**、在会话表的附着判定里，并有独立的行为测试（permissive 和 enforce 两种模式下都要 403）。udid 来自服务端 verdict，从不信客户端自报（与现有归因规则一致）。
- **只对可信 udid 声明能力**：未鉴权连接、以及 verdict OK 但 udid 为空的连接，都无从绑定 → 不声明 `resume=1`。
- **吊销联动（按连接，不按设备）**：`authSessions` 的 kill（SvcExp 过期 / Center 明确拒绝 / grace 耗尽）作用于**一条**传输连接（`wire/session.go` `reviewSession`），每个 listener 各有自己的注册表。续传与之对齐：kill 一条连接时，**当前附着在这条连接上、或最后一次附着在这条连接上且仍处于 detached 的会话**全部标记为 terminated（立即关上游，之后的 `0x06` 回 `403`）。同一 udid 的其他活连接照旧由各自的 authSession 审查。
  - 为什么够：续接必须在一条已过鉴权门的新连接上提交。被吊销的设备若持有效 cookie 重连，今天就能进来，并会在 ≤1 个审查窗口内被 sweeper 再次 kill；续传没有扩大这个窗口，只是让它能把旧会话接回来，而这些旧会话已在第一次 kill 时被终结。
  - kill 回调要能触达**节点级共享会话表**（两种 listener 共用），这是 builder 需要接上的线。
  - **顺序必须是先标记、后断开**：kill 回调先在会话表锁下把相关会话标记为 terminated，**然后**才切断传输连接（QUIC `CloseWithError(0x03, …)`、TCP-WS `sess.Close()`）。反过来的话，客户端看到连接关闭（触发 2）会立刻在另一条已鉴权的连接上发 `0x06`（cookie 命中会跳过 validator），续接可能赶在标记之前落地，会话就在新连接上活下来了。terminated 检查是附着判定的一部分，在同一把锁下原子完成。
  - 客户端看到吊销关闭码（§5.2）时不续接该连接上的 rconn——这是礼貌性的配合，不是防线；防线是服务端的 terminated 标记。
- **每 udid 上限**：会话数默认 512（attached + detached + linger 都计入），字节数默认 64 MiB（§7），任一超出时新 `0x05` 回 `503` 降级。防止单设备占满全局缓冲。**k2r 路由器例外**：一个路由器 udid 代表整个局域网，按默认上限会长期降级为 `0x01`；服务端对 verdict 标为路由器模式的 udid 使用放大 8 倍的上限（是否已有可用的模式标记、标记从哪来，由 builder 核实后接上；没有就先按默认值，并在 `resume-summary` 里统计被 503 的次数）。
- **connID 熵**：128 bit 随机 + udid 绑定 + 必须在已鉴权连接上提交，满足 A5。connID 不出现在 INFO 日志里（DEBUG 也只打前 4 字节）。

---

## 9. 与现有机制的交互

| 机制 | 变化 |
|---|---|
| per-request fallback / data-path distress | 只作用于 opening 阶段，语义不变。续接请求**不**记入 `recordQUICDataPath`（它不是新请求，记入会让一次迁移伪造出一串 timeout） |
| 通道可用性记分（`recordDialOutcome`） | 续接的**传输层**结果计入它所走通道的可用性样本，但**按底层连接尝试计一次，不按 rconn 计**：一次断开会让 N 条 rconn 同时续接，逐条记录会把一个事件放大 N 倍，单独就能触发切走。注意今天**不是**这样：`connect()` 失败后，排在 `connectMu` 上的每个等待者都会各自重拨（`wire/quic.go` ~448-480，重检时 `c.conn == nil`），`recordDialOutcome` 也是每次 `TM.DialTCP` 记一次（`transport.go` ~1146）。所以 builder 要在迁移器里加一层 singleflight：一次迁移波次对一个通道只做一次连接建立、共享结果、记一次分，N 条 `0x06` 在其后复用这条连接。续接的 404/403/409 是会话层结果，不计入 |
| `TCPRecentlyHealthy`（健康监测跳过重连的门） | 不计入。rconn 让重连不再有破坏性，这道门的保护价值随之下降，不需要续接来喂它；减少新拨号导致它更少为真，结果只是重连多一点，而重连现在是无害的 |
| 通道选择 FSM、记分板 | 不变。续接走 FSM 选出的 active channel |
| `runProbeOnce` / 健康重连 / re-race | 代码不变；它们关掉的连接会触发 rconn 的连接级关闭信号（§6.2 触发 2），流由 rconn 续上 |
| `core.pipe()` / `pipeWatchdog` | 代码不变。双向都开着的连接不受 watchdog 影响；已半关闭的连接只能用到 watchdog 从第一个方向退出起算的 30 s 里剩下的时间，被强关时按中止处理（§6.2） |
| 服务端计量 `connDeviceKey` | rconn 实现 `wire.ConnDeviceID` / `wire.ConnSessionID`，返回**创建时**承载连接的值（一条逻辑连接的流量留在同一个计量桶里）—— **我的决定**，见 §12 |
| 服务端 Accepting 门（Center stop 判定） | `0x05` 是新流，经 `AcceptTCP` 后照常被拦。`0x06` 不经 `AcceptTCP`，流级不拦。**已知不对称**：stop 期间 TCP-WS 的新原始 TCP 连接在 `server/server.go` `tcpListenLoop` 就被拒，所以只能经 QUIC 续接，经 TCP-WS 续不上。接受这个不对称：stop 是节点下线的过渡态，续不上 = 今天的行为，不值得为此把 TCP-WS 的门下移到流级 |
| TCP-WS 半关闭 | **行为变化**：smux 今天没有半关闭，`smuxStreamConn.CloseWrite` 等于整个 `Stream.Close`（`wire/tcpws.go` ~90）；rconn 的 FIN 帧让 TCP-WS 上第一次有了真正的半关闭。应是改进（与 QUIC 行为对齐），但 contract tests 要有意识地覆盖它，不能当成意外 |
| k2p outbound | 不涉及 |

---

## 10. 观测

遵守 DIAG 宪法，新增并登记到 `k2/docs/diag-events.md`（`k2/lint/diag_events_test.go` 强制）：

| 事件 | 端 | 级别 | 字段 |
|---|---|---|---|
| `DIAG: conn-resume` | 客户端 | INFO | fromChannel, toChannel, gapMs, attempts, retxBytesUp, retxBytesDown, trigger (`stream-error`/`transport-retired`/`channel-switch`) |
| `DIAG: conn-resume-fail` | 客户端 | WARN | reason (`deadline`/`404`/`403`/`revoked-close`/`no-transport`), gapMs, attempts（`409` 不是失败，不记） |
| `DIAG: resume-summary` | 服务端 | INFO，每 5 min，窗口增量 | opened, resumed, expired, revoked, rejected503, detachedNow, bufMB |

客户端 heartbeat 追加（只追加，不改现有字段）：`rconns`（当前 rconn 数）、`resumes` / `resumeFails`（窗口增量，与 `dial*` 同约定）。

工单诊断时的判据：误报切换后看到 `conn-resume` 而非一串 `proxy-dial-fail` / `pipe-watchdog` = 本设计生效。

### 10.1 上线成功指标

放量后按这几项判断是否达成目标，不达标就停止放量：

| 指标 | 来源 | 目标 |
|---|---|---|
| 续接成功率 `resumes / (resumes + resumeFails)` | 客户端 heartbeat 汇总（device log 抽样） | ≥ 95% |
| 续接 gapMs P90 | `DIAG: conn-resume` | ≤ 3 s |
| 每次 transport-switch / 健康重连之后 60 s 内的 `pipe-watchdog` + `proxy-dial-fail` 数 | 客户端日志，与放量前同版本对比 | 下降 ≥ 70% |
| 「连接不稳定 / 无法连接」人工工单占比 | 工单系统，放量前后各 4 周 | 下降，且无新增"连接卡住不断开"类投诉 |
| 节点 k2s RSS 峰值 | 节点监控 | 不超过放量前 + 全局上限 |
| 正常路径吞吐（nslab 基准） | §11 | ≥ 现状 95% |

---

## 11. 测试与验证（builder 必须覆盖）

**单元**
- 帧编解码 fuzz（含畸形 varint、超长 len、未知帧类型 → RST）。
- 重复字节丢弃：重发区间与已收区间部分重叠、完全重叠、跨帧边界。
- FIN / RST / 承载流出错三种结束的区分；双向 FIN 后立即 `Close()` 走 linger 而**不**发 RST（锁住 §5.4 的误判回归）；linger 期间承载流断开仍能送达尾部字节。
- 授信流控：授信用完时 Write 阻塞、WINDOW 到达后恢复；发送方永不超授信发送（超授信 DATA → RST）；上层不读下载时上传仍推进；**锁住 §5.3 的死锁场景**（信号量被慢读下载占满 + 一条"写完才读"的上传，上传必须完成）；iOS 配置下 64 条慢读连接的总缓冲 ≤ 8 MiB；预留池耗尽时新连接走 `0x01`。
- 大批同时 detach：1000 个会话同时断开时服务端内存增量 ≈ 断开瞬间的在途字节，且其余 attached 会话吞吐不受影响。
- 显式 offset：空洞 → RST；非法续接偏移 → RST；RST 前已收的 DATA 照常交付（错误 response 不丢）。
- deadline 不转发到承载流：上层 Read deadline 超时后 rconn 仍 attached。
- 降级切换驱动迁移：quic→tcpws → QUIC 上的 rconn 全部迁移（错峰）；tcpws→quic 回切 → **不**迁移；re-race 的 `ReplacePrimary`→`SwapTCPWS`→`SetFallbackActive` 序列在 250 ms 静默期后只按最终状态动作一次；`selState.active` 的 AST 守卫。
- 续接握手：客户端取 `clientRecvOffset` 快照后旧流不再发 ACK（合法迁移不被偏移校验判 RST）。
- 绑定键：同一连接上第二个 metadata 流换了 udid，`0x06` 仍按首次鉴权的 udid 判定。

**集成（wire contract tests 新增）**
- 大文件下载 / 上传过程中分别执行：`runProbeOnce` reset、`ResetConnections`、`ReplacePrimary`、强制 QUIC→TCP-WS、TCP-WS→QUIC、杀掉传输连接、模拟源端口/IP 变化 —— 全部以两端 **sha256 一致**为通过标准。
- 宽限期过期 → 客户端得到 reset；服务端上游已关、会话已删。
- `attemptSeq`：尝试 1 附着但回复丢失 → 尝试 2 成功；迟到的尝试 1 拿 `409` 且不影响连接；陈旧承载流上的帧被丢弃。
- 中止清理：opening 超时 / 未 ACK response 前断开 → 服务端立即关上游、不进 detached；detached 时客户端放弃 → ABORT 到达后服务端立即释放。
- udid 绑定：permissive 与 enforce 两种模式下，异 udid / 空 udid 的 `0x06` 都得 `403`。
- 记分去重：一次断开导致 N 条续接（含首次 `connect()` 失败、等待者重拨的情况），`recordDialOutcome` 只记一次。
- 宽限期到期：上游收到 RST 而非 FIN。
- `0x05` 的 `409` / `503` 在 wire 层转成 `0x01` 重开，engine 看不到错误。
- 吊销：kill 后 `0x06` 得 `403`，上游立即关闭；并构造"客户端收到关闭后立即在另一连接上续接"的竞态，续接必须失败（锁住先标记后断开的顺序）。
- 兼容矩阵：新客户端 × 老服务端、老客户端 × 新服务端、任一端总开关关闭 —— 全部退回 `0x01` 且现有 contract tests 不变绿→红。
- 鉴权门 AST 测试覆盖 `0x06`。

**环境**
- `test/netem/nslab.sh` 的 #3747/#3757 形态（**重点验证切换驱动迁移**：大下载进行中开始丢 bulk UDP，选择器切到 TCP-WS 后下载在约一个 RTT 内迁移并完成，不必等 30 s 的 probe reset，sha256 一致）+ 新增"9 s 全断后恢复"形态（复刻 10/27 08:12），量 `conn-resume` 的 gapMs 分布与应用侧零错误。
- 用 nslab 实测确定 §7 的窗口与缓冲默认值（目标：单连接与多连接吞吐都不低于现状的 95%，满足 A4；移动端多条大传输并发时的下降要量出来交给 David 定）。
- iOS 真机：并发 64 条上行压测 + 64 条慢读下载时 NE 峰值内存，确认不触发 jetsam（A3）。
- 用户 6365 的场景回放：低流量 + 周期性误报 critical，确认 chatbox 类长连接不再断。

---

## 12. 我做出的决定（David 可推翻）

1. 续传只覆盖 TCP 代理流，UDP / DNS 不做（N2）。
2. 只同节点续传（N1）；换节点时立即关闭，不尝试。
3. opening 阶段（未收到 response）不续传，保持现有拨号失败语义与 data-path 记账。
4. 计量键用创建时的 session，不随承载连接变化。
5. 能力只对 verdict OK 且 udid 非空的连接声明；其余连接不可续传。
6. 吊销按**连接**终结会话（与现有 kill 的作用范围一致），不扩大到整台设备。
7. 宽限期默认服务端 60 s、客户端截止 = 宽限 − 5 s；窗口与缓冲数值见 §7，服务端和路由器按内存比例取值，均为待实测的起点。
8. 总开关：服务端 `K2_RESUME=0` 不声明能力；客户端 `K2_RESUME=0` / ClientConfig 字段（命名由 builder 定）不使用 `0x05`。
9. 双向 FIN 已交换后的 `Close()` 进入 linger 等我方 FIN 被确认；其余情况下的 `Close()` 按中止发 RST（两端 `pipe()` 不改，靠调用顺序区分）。
10. 续接的传输层结果计入通道可用性记分；不计入 data-path distress，也不计入 `TCPRecentlyHealthy`。
11. 退役检测挂在承载连接的关闭信号上，不挂在 `TransportManager` 的方法上；另加**只在 quic→tcpws 降级时**的迁移（`selState.active` 唯一写入点发通知 + 250 ms 静默期），回切不迁移，不做基于 ACK 停滞的自主判断。
12. 接受 Accepting 门在 TCP-WS 上的不对称（stop 期间只能经 QUIC 续接）。
13. 流控用接收方授信（WINDOW 帧），授信前先从全局信号量预留；窗口上限由接收方按平台定，等于承载传输现有的单流窗口；每条 rconn 有 64 KiB 保底授信。
14. 续接冲突按 `attemptSeq` 新者胜，`409` 非致命。
15. 吊销先标记 terminated 后断连接；udid 绑定检查不依赖 enforce 模式。
16. opening 阶段与 response 被 ACK 之前的断开按中止处理；detached 放弃时尽力发 ABORT。
17. DATA 帧显式携带 offset。
18. detached 时服务端不预读上游；宽限期到期以 RST 关上游。
19. 续接绑定 `authSession.udid`（不可变），不用 `ctx.dev`。
20. k2r 路由器 udid 的每 udid 上限放大 8 倍（前提是服务端能识别路由器模式）。

## 13. 留给 builder

- rconn 内部锁设计（跨包嵌套需加入 `k2/CLAUDE.md` 锁序链，包内锁写进 `k2/wire/CLAUDE.md`；遵守"临界区只读写字段"）、goroutine 结构、缓冲块池实现。
- 承载连接关闭信号的具体接法（QUIC `conn.Context()`、smux session 关闭通道如何暴露给 rconn，不破坏封装规则）。
- 节点级共享会话表放在哪个对象上、如何同时交给两个 listener 和 kill 回调。
- ClientConfig 开关字段命名，以及是否需要 webapp 透传（默认不透传）。
- 发布顺序：先全量升级 k2s（能力默认开启），再随客户端版本放量；回滚只需关服务端开关。
