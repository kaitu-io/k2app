# Relay 可靠性内核重设计

- **日期**: 2026-07-11
- **范围**: `k2/wire/RelayManager` 的选路/健康内核 + 三处配套改动
- **前置**: [`2026-06-25-antiblock-node-relay-camouflage-design.md`](2026-06-25-antiblock-node-relay-camouflage-design.md)(伪装中继基础)、本 session 已实现的 `RelayManager`(单主机 + 连接复用 + `relay-add-nodes` 增量喂节点)
- **目标**: 卓越且可靠;**在 keypoint 上做简单**——复杂度越高,我们自己越容易写错。

## 1. 问题

Relay 是控制面在 direct 被 GFW 封锁时的兜底(仅 `markDirectBlocked` 粘性态启用),因此**永远工作在对抗环境**。给定约 100 个伪装节点 `{ip,pin,ech,score}`、串行探测、每次试错都花用户最多 2.5s,要在动态封锁(此刻好的可能被封、被封的可能恢复)下**快速找到一个能用的节点**,同时:

- 不因整网中断/captive portal 把一大批健康节点误标为坏(相关性失败)。
- 不永久封杀恢复了的节点(非平稳自愈)。
- 不引入后台主动探测(流量 + 指纹)。

当前实现用 `successN/failN/consecFail/latencyEWMA + rankedSnapshot(固定 unhealthyThreshold)`:无衰减、无回探 → **恢复了的节点会被永久封杀**;且一遇错就跳下一个节点 → **死掉的 keep-alive 连接会害我们丢掉本来好的节点**。本设计**取代**该健康/排序模型。

## 2. 确定的约束(用户已拍板)

- **严格单主机、串行、无并发**。不做对冲(hedging)。
- **冷启动仅持久化,不预热**。不做主动握手。
- **无后台主动探测**。

## 3. 内核:一个 active + Center Score + 会过期的惩罚盒

### 3.1 状态(最小)

```
全局:  active *node                 // 最近一次「往返成功」的节点,至多一个;sticky 优先
每节点: blockedUntil int64 (unix)    // 惩罚盒到期时刻,0=未罚
        Score        float64         // = Center recommendScore,仅作未知节点排序的弱先验
```

**没有** `lastGood` 排序键、`successN/failN`、`consecFail`、`latencyEWMA`、UCB、衰减曲线。除了那一个 `active`,**不对其余 99 个维护任何客户端历史**——因此「久未探测的节点知识陈旧」这个问题**不存在**:它们永远按 Center Score(服务端持续刷新的当前值)排。

### 3.2 Center Score 的定位(诚实)

`Score` = Center 的 `recommendScore`(`[0,1]`,由 webapp 经 `relay-add-nodes` 喂入)。它衡量的是**节点配额余量/配速**(把用户从接近月度带宽上限的节点引开),**与「节点是否被 GFW 封、连不连得上」几乎正交**;且 relay 控制面流量不消耗节点配额。故 `Score` 只是**弱先验、决定盲走起点**,不是真相。未知节点之间的排序其实低风险——真相由惩罚盒的实测决定。保持按 Score 排(免费、已接好、够用),不引入轮转/打散(YAGNI)。

### 3.3 选择顺序(一个函数)

```
now := unix()
1. 若 active 非空且 active.blockedUntil <= now  → 先试 active(复用热连接)
2. 其余节点中 blockedUntil <= now 的,按 Score 降序
3. 跳过仍在惩罚盒内(blockedUntil > now)的节点
4. 若上述可选集为空(全部被罚)→ 忽略惩罚,全体按 Score 降序硬着头皮试
按此顺序串行尝试,首个**往返成功**即返回(判定见 §3.4)。
```

### 3.4 两个正交信号(correctness 级)——链路健康 vs 谁当 active

我们**信任自己的节点**:一旦到节点的 pinned 链路建成,节点之后到 Center 那一跳是节点的职责,不该反过来污染「这个 IP 被墙没」的判断。因此把判定拆成两个正交信号:

| 信号 | 定义 | 驱动什么 |
|------|------|---------|
| **链路健康** | 到节点的 **pinned uTLS+ECH 握手完成**(或复用了一条已建成的连接)| 惩罚盒 / 候选资格 / 「本轮网络是否活着」 |
| **往返成功** | `client.Do` 返回了一个 HTTP 响应信封(**任意状态码**,含 4xx/5xx)| 谁配当 sticky `active` |

- **链路健康是主信号,面向 GFW。** 审查者的封锁——IP 黑洞、握手中 RST、TLS 指纹封——全在握手期发作。握手完成 ≈ 审查者放行 + 对端确是我们的节点。**TCP 有 ACK 不算**——ACK 之后 GFW 仍可 RST;必须是 pinned 握手完成。
- **往返只决定偏好,不扣血。** 握手成功但请求没拿回信封(node→Center 抖动 / Center 5xx 无响应 / 节点出口断):**链路健康不受影响**(不进惩罚盒),但该节点**这次没资格当 active**,本请求换下一个去兑现。
- **净效果**:Center 宕机 / API 5xx / API 慢,**不会**把好节点打进黑名单——健康信号对 Center 可用性免疫。broken-egress 节点(TLS 通、出口断)握手成功不挨罚,但因往返从不成功 → 永远当不上 active → 不会被优先粘住,只按 Score 待在候选池。
- **实现**:用 `httptrace`(`TLSHandshakeDone` / `GotConn.Reused`)区分「链路是否建成」与「往返是否成功」。链路建成即置 `anyLinkOK=true`。

### 3.5 更新语义 —— 相关性失败保护(核心可靠性)

**判别器 = 本轮有没有任何一次握手成功(`anyLinkOK`)。** 握手成功证明「本地网络这条路是活的」(与 Center 无关)→ 本轮的握手失败才干净可信 → 提交惩罚。整轮无一握手成功 = 被本地网络状况混淆(断网 / captive portal / 整池 IP 被封)= **health-state no-op**。

```
// 前置防重扫(见下):若刚发生过一次「全轮握手皆挂」,短时间内直接快失败
if now - lastAllLinksDownAt < RESWEEP_BACKOFF { return networkError }

var linkFailed []*node
anyLinkOK := false
for n := range 选择顺序 {
    conn, herr := dialPinnedHandshake(n)      // 复用连接时握手是 no-op,herr=nil
    if herr != nil { linkFailed = append(linkFailed, n); continue }  // 链路失败 → 缓冲
    anyLinkOK = true                          // 网络被证明活着(Center 无关)
    resp, rerr := roundTrip(conn, req)        // GetBody 已设 → stdlib 对已死复用连接安全重试
    if rerr == nil {
        commit(linkFailed)                    // 有链路活 → 缓冲的握手失败可信,落地惩罚
        active = n; persistIfActiveChanged()  // 往返成功 → 赢得 sticky active
        return resp                           // 任意 HTTP 状态码
    }
    // 握手 OK 但请求没兑现:链路健康不动,该节点不当 active,换下一个去兑现
}
if anyLinkOK {
    commit(linkFailed)                        // 网络活着,握手失败仍可信 → 落地
    return centerError                        // 链路都通、往返都没成 → Center 侧问题
}
// 整轮无一握手成功 → 混淆 → 零惩罚、active 不动
lastAllLinksDownAt = now                      // 记一笔,触发前置防重扫
return networkError
```

- **握手失败**只在 `anyLinkOK` 时才落地惩罚;**往返失败**永不惩罚(顶多让节点当不上 active)。
- **故意不接** OS 网络状态(`NetEvent`/`netCoordinator`):「有握手成功」是端到端地面真相,能兜住 OS 误报「已连接」的 captive portal——更准、少一处耦合。
- **前置防重扫(Linus #2)**:整轮握手皆挂时记 `lastAllLinksDownAt`;`RESWEEP_BACKOFF = 2s` 内的后续 `Fetch` 直接快失败,不再重跑同一套注定失败的 sweep(每次可烧到 9s)。防断网/整池被封时的活锁。第二个、也是最后一个旋钮。
- 接受的局限:网络好但**整池真被封**时无一握手成功 → 零惩罚 → 交上层(webapp relay→direct / `markRelayUnsupported`)。**绝不产生假黑名单**;区分「整网断」与「整池被封」既做不到也不需要,两者正确动作相同。

### 3.6 惩罚盒自愈

握手失败提交时 `blockedUntil = now + PENALTY`(`PENALTY = 180s`,固定)。惩罚**自动过期** → 节点重新进入候选 → 被下一次真实请求按需回探 → 握手成功则脱罚(往返再成功才当 active)、握手仍失败则再罚 180s(近似退避)。**永不永久封杀。** 持续被墙的节点至多每 180s 被回探一次。

> 全设计共 **两个旋钮**:`PENALTY=180s`(惩罚时长)、`RESWEEP_BACKOFF=2s`(整轮全挂防重扫)。超时常量(2.5s/9s,改动 2)是预算不是调参。

## 4. 配套改动

### 改动 1 — 靠 stdlib 安全重试 stale keep-alive(不手写重试)

动机:复用了一条已被 GFW/NAT 静默 RST 的 keep-alive 连接时,写请求会失败——若因此把 active 换掉,就是被死连接害了。**解法不是手写重试**:Go 的 `http.Transport` 对「复用连接 + 请求字节尚未写出」这种可证明安全的情况,**本来就会自动换新连接重试**(重走我们的 `DialTLSContext`),且对 POST 也如此(没写出就安全)、而对「字节已写出」的非幂等请求**拒绝**重试——正好避免双花。

- **要做的**:给 `hreq` 设 `GetBody`(body 是 string,可回绕)→ 打开 stdlib 的这条安全重试路径。
- **不做的**:任何自定义「同节点重试」逻辑(会用更不安全的方式重新发明它,还可能双花非幂等 POST)。
- 超时/黑洞(等满 timeout,已写出字节):stdlib 不重试 → 归为往返失败 → 换下一个节点兑现。

### 改动 2 — 收紧超时

- per-node 单次尝试:`5s → 2.5s`(热连接约 200ms,超时只咬冷/坏节点)。
- 整体 `Fetch` 预算:保持 `9s`,与 webapp `RELAY_TIMEOUT_MS` 对齐。

### 改动 3 — 持久化 active

- Go 自持状态文件(k2r `state.json` 模式,权威留在 Go,不让 webapp 往返):内容仅 `{activeIP, updatedAt}`。
- **写**:仅当 active 发生变化时写一次。
- **读**:进程启动 `AddNodes` 后,若持久化的 `activeIP` 在池内则置为 `active` → 重启后第一发直接打上次的赢家(仍付一次握手,符合「不预热」)。
- **plumbing(唯一跨层接线点)**:状态文件路径注入——桌面用 daemon state dir,移动端复用 appext 已有的数据目录(若 appext 目前未接收路径,加一个 init 参数)。

## 5. Non-goals(刻意不做)

- ❌ 对冲 / 并发请求(严格单主机)
- ❌ 主动预热握手 / 后台主动探测
- ❌ 延迟 EWMA 排序、UCB 探索、衰减曲线、`consecFail`
- ❌ 对 99 个未知节点做主动保鲜(接受「用到才盲走发现」,有界 + 自愈)
- ❌ 自定义同节点重试(改用 stdlib `GetBody` 安全重试,见改动 1)
- ❌ 往返失败反哺链路健康(broken-egress 节点靠「当不上 active」自然沉底,不额外加 consecFail 计数)
- ❌ 完整熔断器(只留 `RESWEEP_BACKOFF=2s` 轻量防重扫,防活锁)
- ❌ OS 网络状态接线
- ❌ 未知节点轮转/打散(保持 Score 序)

## 6. 测试(TDD,全部 desk-side)

| 测试 | 断言 |
|------|------|
| `ActiveNodeIsSticky` | 往返成功后连续请求只握手 1 次(复用),始终打 active |
| `PenaltyBoxExpiresAndReprobes` | 握手失败节点 180s 内被跳过,过期后重新入选(自愈) |
| `FailoverSkipsPenalizedNode` | 被罚节点本轮被跳过,落到下一个 |
| `UnknownNodesOrderedByScore` | 全未试时按 Score 降序 |
| `HandshakeIsHealthSignal_NotRoundTrip` | 握手成功但请求失败(Center 5xx/无响应)→ 节点**不进惩罚盒** |
| `OnlyRoundTripSuccessEarnsActive` | broken-egress 节点(握手成功、往返总失败)永远当不上 active |
| `HandshakeSuccessValidatesSweep` | 有握手成功即视网络活着 → 缓冲的握手失败落地惩罚 |
| `AllLinksDownSweepPenalizesNothing` | 整轮握手皆挂 → 零惩罚、active 不变、返回 network error |
| `ResweepBackoffAfterAllLinksDown` | 全挂后 2s 内的 Fetch 直接快失败,不重跑 sweep |
| `CenterDownDoesNotBlacklistNodes` | 所有节点握手通但 Center 全 5xx/无响应 → 无一节点被罚 |
| `StaleKeepAliveRetriedSafelyByTransport` | 复用死连接(字节未写)→ stdlib 换新连接重试成功,active 不变 |
| `PersistAndRestoreActive` | 写 active IP → 新建 manager + AddNodes → 该节点被置为 active、排第一 |
| `PersistWritesOnlyOnActiveChange` | 连续命中同 active 不重复写盘 |

既有 6 个 `RelayManager` 测试(增量/保留健康/连接复用/顺序 failover/header 透传等)中,与旧健康模型绑定的需随内核重写调整,其余保持绿;全层回归保持绿。

## 7. 风险 / 信心

- **代码信心目标 9.5/10**:纯 Go 内部逻辑 + 一个状态文件,TDD + `-race`。两个旋钮(180s/2s),重试与安全性外包给 stdlib。
- **业务信心**:控制面路径,无真机 smoke 封顶 6-7。需 Android 登录态冷启动 logcat 收尾(与既有 relay 真机待办合并跑)。
- **主要风险点**:①改动 3 移动端路径注入的接线;②`httptrace` 正确区分「链路建成」与「往返成功」——这是两信号模型的判定基石,须有针对性测试(`HandshakeIsHealthSignal_NotRoundTrip` / `CenterDownDoesNotBlacklistNodes`)。其余为纯内部逻辑,低风险。
- **已知局限**:broken-egress 节点(TLS 通、出口断)不挨罚,靠「当不上 active」自然沉底;若日后观测到这类节点反复被 Score 选中兑现失败,再考虑加往返失败的软降级(现在 YAGNI)。
- **部署**:relay 尚未上生产,协议契约可直接换,无兼容桥。

## 8. 实现顺序(交 writing-plans 细化)

1. 重写 `RelayManager` 状态与 `Fetch`:两信号判定(`httptrace` 分链路/往返)、惩罚盒、`anyLinkOK` 缓冲提交、防重扫、`GetBody`(改动 1)。红→绿。
2. 收紧超时常量(改动 2:2.5s/9s)。
3. 状态文件读写 + 路径注入(改动 3),含 appext / daemon 两处接线。
4. 全层回归 + `-race` + gofmt/vet + plugin-purity-check。
