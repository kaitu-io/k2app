# 节点故障自动切换（F2）设计

日期：2026-11-05 · 状态：待批准 · 来源：k2app-0c 交接（F1 = commit febcf6396）

## 1. 为什么做

节点挂掉之后，客户端不会自动换节点：

- **桌面（macOS / Windows / Linux daemon）**：引擎 Phase-B 向 `OutboundProvider` 要替换 URL。daemon 装的 provider 是 `subscription.Manager`（`k2/daemon/daemon.go:545`），只认 `k2subs://`；webapp 传的是普通 `k2v5://`，所以 `NextURL` 返回 `ErrNoSubscription`。`outboundReplaceLoop`（`k2/engine/engine.go:2036`）随即 `OnOutboundFatal` + `e.fail()`，隧道断开。现场 8/8 失败。
- **桌面附带误报**：上述错误文本含 `subscription`，`k2/engine/error.go:227` 的子串匹配把它归为 **402 订阅到期**。PVE netem 实测 `["disconnected",402]`（旧版 +121s、F1 后 +50s）。用户会以为要续费。
- **移动（iOS NE / Android VpnService）**：appext 不装 provider（`k2/appext/event.go:38`）。`provider == nil` 时，引擎对同一个死节点无限 Phase-A re-race，永远不换节点。
- **F1 的影响**：F1 让死掉的 TCP-WS 更快触发 re-race（+86s → 约 +15s），所以 Phase-B 和上面的失败会更常、更早发生。

影响面目前不大（403 次有评分的会话里有 29 次差评，其中 2 次有 transport-rerace），但 F1 会把它放大，402 误报还会直接误导付费用户。

## 2. 目标 / 非目标 / 反目标

**目标**
- G1：Auto 模式下，当前节点不可达且**用户本地网络正常**时，无感（不重建 TUN）切换到另一台机器上的节点，桌面和移动行为一致。
- G2：能可靠区分"节点坏了"和"用户自己的网坏了"（含 Wi-Fi 连着但不能上网、网页登录型 Wi-Fi），并给用户不同的、准确的提示。
- G3：任何节点故障都不再报 402。
- G4：判定与时序在测试和故障注入中完全可复现，每次判定在现场日志里留痕。

**非目标**
- 手动选择的节点不切换：挂了就在原节点重试，尊重用户的选择（已确认）。
- 网页登录型 Wi-Fi 只负责提示，不帮用户完成登录（VPN 开着时登录页流量也走隧道，单独立项）。
- 连接过程中不刷新候选名单（名单在每次连接时生成，下次连接重新挑）。
- 不改服务端打分（`recommendScore`）和 k2subs 路径。
- 不改 k2r 网关：`gateway/gateway.go:602` 的 `outboundProviderFor` 对普通 `k2v5://` 已经返回 nil，避开了这个失败，保持现状（我的决定）。

**反目标（出现即算失败，哪怕"技术上能用"）**
- 用户网络只是短暂中断（电梯、切 Wi-Fi），隧道却被断开了。这比现在的移动端更差。
- 界面长时间显示"已连接"，实际隧道不通。
- 用户本地网络断了，客户端却把好节点当死节点划掉。
- 非付费原因出现 402。

## 3. 考虑过的方案

| 方案 | 做法 | 取舍 | 结论 |
|---|---|---|---|
| A. webapp 重连 | 引擎报错后 webapp 断开，再用下一个推荐节点重连 | 用户会看到断线；移动端引擎不会报错，要另造信号，两端两套逻辑 | 否 |
| **B. 引擎内候选名单** | webapp 把首选 + 备选写进 ClientConfig；引擎内置静态 provider，复用现有无感替换（`replaceOutboundServer`，不重建 TUN / DNS / 规则引擎） | 要改 ClientConfig 跨层契约（Go、TS、contracts）；换来两端共用一套、用户不掉线 | **采用** |
| C. 不 fail | provider 返回非 k2subs 时当作"没有 provider"，一直重试 | 界面显示已连接但不通，比现在更差 | 否 |

名单用完之后：在"确认用户网络正常"的前提下**断开并报 503**，不做"循环重试、永不断开"。理由：此时真正的情况是"用户能上网但所有备选节点都连不上"。假装已连接会让用户误以为 VPN 在工作（参考工单 #3169，UI 已连接、隧道已死 11–58 分钟，当时按 bug 处理）。网络断开的情况不走这条路（见 §5），所以不会造成"电梯里被断开"。

## 4. 已定的设计

### 4.1 候选名单（webapp）

- **只在 Auto 模式生成**（`connection.store.ts` 约 478 行，`serverMode === 'manual' && selectedCloudTunnel === null` 分支，即 `pickAutoTunnel` 的调用点）。
- **数量**：首选 1 个 + 备选最多 3 个。每个死掉的备选最多花 `wire/race.go:16` `raceTimeout = 5s`，最坏多等约 15s。
- **挑法**：重复调用 `pickAutoTunnel`，每次排除**已选中节点所在的物理机器**（同一台机器的多条 tunnel 会一起挂）。机器用 `tunnel.node` 识别（`SlaveNode`，见 `webapp/src/services/api-types.ts` 约 205 行的 `ipv4` / `ipv6`；用哪个字段作为机器唯一键由实施者确认，必须稳定唯一）。候选池不足 4 台机器时，有几个算几个，0 个备选也合法（行为退化为"不切换，原节点重试"）。
- 国家过滤（`excludedCountries`）对备选同样生效。
- 手动选择：不生成备选。

### 4.2 ClientConfig 契约

- **放在 ClientConfig 顶层，以首选 URL 为键**（我的决定）：形如 `fallbacks: { "<首选 k2v5 URL>": ["<备选1>", "<备选2>", "<备选3>"] }`。原因：`buildConnectConfig` 会把同一个 `serverUrl` 写进多条 route（分应用代理覆盖 + 最后的 `all`），引擎按 URL 去重出口，`NextURL(currentURL, …)` 也只带 URL，不带 route 身份。所以按 URL 挂备选和引擎的模型一致，放在 RouteConfig 上则会出现同一出口多份、可能互相矛盾的名单。精确字段名按现有惯例定（Go 发 snake_case，桥接层转 camelCase）。
- 只允许 `k2v5://`；其他 scheme 在配置校验时丢弃并打 WARN。`k2p://home` 等非云出口不参与切换。
- **连通性检查地址也走 ClientConfig**（我的决定）：新增顶层 `netcheck: [{url, status, body?}]`（计划阶段改为对象：Apple 与自有地址是"200 + 固定正文"，不是 204），由 webapp 从 `webapp/src/brands/<brand>/index.ts` 填入。k2 本身没有品牌概念，品牌字面量按根 CLAUDE.md 只能放在品牌文件里。字段缺失或为空时视为"未知"，行为与 F2 之前完全一致：不做判定门、不等网、不显示 106，照常换节点，耗尽报 503（401/402 保持）。网卡断开（101）仍会等网。（实施阶段改动，原设计是按情况 2 等网：终审发现没有检查地址的调用方实际只有 k2r 网关和 CLI，等网会让路由器空闲时卡在 106、不再轮换节点；webapp 总会下发检查地址。代价是没有检查地址的客户端在断网时仍会像以前一样把备选试完并报 503。）和 `fallbacks` 是同一次契约改动。
- 检查地址的 DNS 用 ClientConfig 里配置的直连 DNS。实施者要确认 overleap 下这组 DNS 不是只有大陆地址（`core/dns/direct.go` 的默认值全是大陆解析器）；如果是，就在同一次改动里让 webapp 按品牌下发。
- **凭证**：备选 URL 同样用 `authService.buildTunnelUrl` 生成，和首选带同一个 token，有效期相同，存储位置相同（iOS `providerConfiguration`），所以没有引入新的暴露面。切换到备选时如果 token 已失效，得到的是真实的 401，按原样报出，不属于误报。
- 三处同步：Go `config.ClientConfig`、TS 类型、`contracts/` 下对应的金样（按 `webapp/CLAUDE.md` 契约门流程重新生成，不手改）。
- 老客户端不发这个字段，新引擎照旧（无备选）；新 webapp 配老引擎，字段被忽略。两个方向都兼容，不需要 bridge 版本门。
- **修订 R1（2026-11-05，David 批准；覆盖本节"耗尽报 503"、§4.4 表最后一行、§5.1 情况 4"都失败则断开"、§5.3 规则 2"报 503 并 `e.fail()`"）**：用户网络正常时候选耗尽**不再立即断开**。五轮修复每轮都又找到一条"节点活着却 fail(503)"的路径，根因是任何一次"节点已死"的误判都会直接变成断开。现在耗尽（含一个都没试成、候选全部无码拒绝）一律进入现有的重试表：状态 connected，显示 reconnecting + 503「…all replacement candidates failed; retrying」，按 5s→10s→30s→30s 退避，每次先 Phase-A 再 Phase-B（Reset 排除集），任一成功即离开重试表。只有**同一出口在网络正常时连续耗尽满 10 分钟**（`exhaustFailAfter`）才走原来的 `OnOutboundFatal` + `fail(503)`（k2r / 桌面网关靠 `OnOutboundFatal` 重建会话）；离线期间、以及当前节点以无码拒绝作答（节点活着、Center 不通）的那几轮不计入，计时从下一次在线耗尽重新开始。账户结论仍然立即断开：Phase-A 的有码 401/402/403、粘住的 401/402 lastError、或本轮试过的候选**全部**是有码 401/402/403（部分有码、部分不可达则进重试表，到上限时报那个码）。手动模式 / 无备选（`ErrNoCandidates`）不变：永远重试，没有上限。代价：整个机群真死时用户看到 10 分钟"重新连接中"才断开，而不是立刻断开。日志：`DIAG: outbound-exhausted` / `transport-rerace reason=exhausted-retry` / `outbound-exhausted-recovered` / `outbound-exhausted-fatal`（`k2/docs/diag-events.md`）。

### 4.3 引擎内静态 provider

- 新增一个实现 `engine.OutboundProvider` 的静态 provider，在引擎 `Start` 时由 ClientConfig 的备选列表构造。
- `NextURL(current, reason)`：把 `current` 加入排除集，按顺序返回第一个未排除的备选；全部排除时返回 `ErrCandidatesExhausted`（新的哨兵错误）。
- 提供 `Reset()`：清空排除集（§5.3 用到）。
- **排除有时效**：每条排除记录 10 分钟后自动失效（我的决定）。否则长连接里隔几小时坏一次，排除集只增不减，最终在所有节点都已恢复的情况下报 503。失效后，原首选可以重新成为候选。
- **可测性**：provider 的时钟可注入（测试用假时钟）。
- **组合规则**（计划阶段定为：组合在引擎 `Start` 内完成，daemon / appext 不改；壳层 provider 通过可选接口 `URLOwner.OwnsURL` 声明自己认识的 URL，都不认识 = `ErrNoCandidates` → §5.3 规则 5）：
  - 桌面：route 的 via 是 `k2subs://` → 用 `subsMgr`（现状不变）；否则用静态 provider。最干净的做法是做一个按 URL 分派的组合 provider，由实施者定。
  - 移动：appext 装静态 provider（目前不装任何 provider）。
  - 没有备选：静态 provider 第一次调用就返回耗尽。但 §5 的判定在它之前，不会因此断开（见 §5.4）。

### 4.4 错误码

- 在产生错误的地方直接构造 `EngineError`，不再交给 `ClassifyError` 猜文本（k2 的 Error Ownership 规则）。
- `error.go:227` 收紧：去掉裸词 `subscription` 匹配，只保留明确的付费信号。补测试，钉住"节点耗尽 / `ErrNoSubscription` / 任何含 subscription 字样的非付费错误"都不会变成 402。
- 新增或使用的码（都必须同时登记在 `k2/engine/error.go` 和 `webapp/src/utils/errorCatalog.ts`，并更新相关契约测试）：

| 情况 | 码 | 类别 | 用法 |
|---|---|---|---|
| 网卡全没了 | 101 `NetworkUnavailable`（已有） | network | reconnecting 状态的原因，不断开 |
| Wi-Fi 连着但上不了网 | 新码，暂定 106 | network | 同上 |
| 需要网页登录 | 新码，暂定 107 | network | 同上 |
| 用户网络正常，所有候选都不可达 | 503 `ServerUnreachable`（已有） | server | 断开 |

  106 / 107 是暂定值：webapp catalog 里 100–105 和 108 已占用。实施者要先确认两端都没有占用这两个值。

## 5. 判定：节点坏了，还是用户的网坏了

### 5.1 四种情况

| # | 情况 | 处理 | 用户看到 |
|---|---|---|---|
| 1 | 网卡全没了 | 等网，不消耗备选 | 重新连接中 ·「网络已断开，恢复后自动连接」(101) |
| 2 | Wi-Fi 连着但上不了网 | 等网 | 重新连接中 ·「当前网络无法访问互联网，恢复后自动连接」(106) |
| 3 | 需要网页登录 | 等网 | 重新连接中 ·「此 Wi-Fi 需要网页登录」(107) |
| 4 | 用户网络正常 | 换节点；都失败则断开 | 无感；最终断开时 503「线路暂时都无法连接，请稍后重试」 |

**"等网"必须是引擎自己持有的状态，不能只推送一次事件。** reconnecting 目前是 handler-only 的瞬时推送（`engine/event.go:22`）。等网期间，另外三条路径照样在跑：网络变化（`engine.go` 约 765 行）、`onHealthCritical`（约 820 行）、唤醒（约 888 行），它们都会用 `buildStatusLocked()` 推一个 connected，把等网状态盖掉；daemon 的 `lastStatus` 也会跟着变成 connected，正好落入反目标。做法参照现有的 `paused`（`StatusJSON()` 在 `e.paused` 为真时报 paused）：
- 引擎加一个受 `e.mu` 保护的字段 `netWait *EngineError`，非 nil 表示正在等网，值就是原因码。
- `buildStatusLocked()` 在 `netWait != nil` 时一律输出 `state=reconnecting` + `error=netWait`。这样所有推送路径和轮询（`StatusJSON`、daemon `status`）都自动一致，不用逐个改调用点。
- 只有 `outboundReplaceLoop` 写 `netWait`：进入等网时设置，退出时清空并推一次状态。Stop 时清空。
- 等网期间，上述三条路径的重连仍然可以跑（它们重连的是当前节点，成功了也无害），但不能覆盖对外状态；下一次判定为情况 4 时，由 replace loop 退出等网。

**原因码怎么到达各端界面：**
- 桌面：daemon 透传 status，webapp `vpn-machine.store.ts` 已有 `reconnecting` 状态和"BACKEND_ERROR + retrying → reconnecting"映射，补上带原因的文案即可。
- Android：`K2VpnService` 把 status 转给 K2Plugin。实施者要确认 error 字段能透传到 webapp，并把通知栏改成显示原因。
- **iOS：目前传不到。** `PacketTunnelProvider.onStatus`（约 597 行）收到 reconnecting 只记日志；webapp 的 reconnecting 来自系统的 `NEVPNStatus.reasserting`（`K2Helpers.swift:24`），错误只来自 App Group 的 `vpnError`。需要改原生代码：NE 在 reconnecting 带 error 时设置 `reasserting = true`，并把原因写进 App Group 的一个新键（不要复用 `vpnError`，那个键表示断开时的错误）；K2Plugin 在 reasserting 期间读这个键，附到 reconnecting 状态上。这部分要等下一个 App Store 版本才能生效。在那之前，iOS 显示不带原因的"重新连接中"，判定和切换逻辑本身不受影响。webapp 通过能力检测处理：原因字段缺失就显示通用文案。

i18n 至少覆盖 zh-CN / en；中文文案中不出现 "Kaitu"。

### 5.2 三层证据（按顺序，命中即停）

1. **系统网卡状态**（只有移动端有）：iOS `NWPathMonitor` 的 `unsatisfied`，或 Android `activeNetwork == null`，都会经 `netCoordinator` 写成 `networkUp == false`。命中 → 情况 1，不发任何请求。桌面端没有这层（`daemon/network_monitor.go` 只发 `changed`，nil 接口被跳过），直接看第 2 层。
2. **被动直连证据**：最近 10 秒内，绕开隧道的直连 TCP 连接（DirectDialer，例如智能分流下的国内网站）有一次成功建立 → 情况 4。实现：DirectDialer 成功时记一个原子时间戳，判定时读取。只统计建连成功，不统计数据量。
   - 例外：**连接节点本身的拨号不算**。节点拨号也走 DirectDialer，但它恰恰是被判定的对象；而且节点 IP 被封时，到它的 TCP 也可能"成功"。实施者要在记时间戳的地方把 wire 拨号区分开。
3. **主动检查**（前两层都没结论时）：绕开隧道，并发请求 3 个公开的连通性检查地址，HTTP GET，单个超时 3s，整体以最慢的那个为准，最多 3s。
   - 任意一个返回 **HTTP 204 且 body 为空** → 情况 4。
   - 没有 204，但至少一个返回了 HTTP 应答（3xx 跳转、200 页面等） → 情况 3。
   - 全部无应答（DNS 失败、连接失败、超时） → 情况 2。
   - 地址列表放进品牌配置。kaitu：小米 `connect.rom.miui.com/generate_204`、华为 `connectivitycheck.platform.hicloud.com/generate_204`、vivo `wifi.vivo.com.cn/generate_204`；overleap：`connectivitycheck.gstatic.com/generate_204`、`www.gstatic.com/generate_204`，以及 Apple `captive.apple.com/hotspot-detect.html`。Apple 这个返回的是 200 + 固定正文 `Success`，判定逻辑要支持"期望 204"或"期望 200 + 固定正文"两种。实施前每个地址都要实测一遍：国内地址用中国大陆出口测（testlab），海外地址用海外网络测。另加一个我们自有 CDN 上的 204 地址作为第 4 个（已同意：第三方为主、自有为备）。
   - DNS 解析走引擎现有的直连 DNS（`core/dns/direct.go`，`223.5.5.5` 等），不走隧道 DNS。HTTP 请求必须走 DirectDialer（Android 上还要经 `protect()`），不能进 TUN。
   - 不跟随重定向；不复用连接（每次新建）；请求头不带任何用户标识。

### 5.3 时序规则

所有判定只在 `outboundReplaceLoop` 这个单 goroutine 内同步执行，不引入新的并发写者。

1. **进入 Phase-B 之前判定一次**（即 `streak >= reRaceFailThresholdForReplace` 之后、调用 provider 之前）。情况 1/2/3 → 进入等网（规则 3），不调用 provider，不累加任何排除。
2. **宣布耗尽之前再判定一次**：provider 返回耗尽时，先重新判定。如果此时不是情况 4，说明是在切换过程中断网了，前面被判死的节点可能是冤枉的 → 对静态 provider 调用 `Reset()`，进入等网。只有判定仍是情况 4，才报 503 并 `e.fail()`。
3. **等网**：
   - 推送 reconnecting + 对应码（101/106/107），隧道和 TUN 保持。
   - 恢复触发：netCoordinator 的网络恢复回调（移动端的 available/changed、桌面的 changed）立即触发一次重新判定；没有事件时按 5s → 10s → 30s 退避，之后固定 30s 一次。
   - 判定变成情况 4 后，退出等网：先对**当前节点**做 Phase-A re-race，成功则恢复 connected；失败则按正常流程进入 Phase-B（此时 streak 已满足阈值）。
   - 等网期间情况在 1/2/3 之间变化时，更新推送的原因码。
   - 引擎 Stop / ctx 取消时，立即退出等网，取消正在进行的检查（HTTP 请求要受 ctx 控制）。
   - 等网不设上限，和现在移动端"一直等"的行为一致；用户随时可以手动断开。
   - 等网发生在 `outboundReplaceLoop` 这个单 goroutine 里，期间其他出口的 re-race 也会排队（我的决定，接受）。本地网络状态对所有出口是一样的，网不通时其他出口的 re-race 本来也会失败。等网的退避 sleep 必须能被 `reRaceChan` 之外的 ctx 取消唤醒。
4. **Phase-B 中逐个试备选时不重复判定**（避免每个候选多花 3s），规则 2 兜底。
5. **手动模式 / 无备选**：仍然执行规则 1。判定为情况 4 但没有备选时，**不断开**，保持现状的"原节点继续 re-race"（对手动用户来说，这比报 503 更符合"尊重选择"），并推送 reconnecting + 503 作为原因，让 UI 能显示"节点连接不上，正在重试"。是否这样显示由实施者和 UI 一起定，但不能 `e.fail()`。

### 5.4 状态与旧逻辑的关系

- `provider == nil` 分支（现在的移动端）被新逻辑取代：所有平台都会有 provider（静态的或 subsMgr）。
- `onHealthCritical` 和唤醒重连里已有的 `isNetworkUp()` 门不变。
- 桌面 k2subs 路径同样经过 §5 判定，修复它在断网时把订阅里的节点逐个划掉的问题。

### 5.5 前置修复：Android 网络恢复后状态卡死

`K2VpnService.kt` 在 `onLost` 且 `activeNetwork == null` 时发 `unavailable`，但 `onAvailable` 只发 `changed`（约 650 行，注释说明这是有意的）。`netCoordinator` 在 `networkUp == false` 时会忽略 `changed`（`netmon.go` "changed while network down, ignoring"），而全仓库没有任何地方给 Android 发 `available`。于是一次全断网之后，这次连接里 `networkUp` 一直是 false：恢复重连、唤醒重连、health-critical 重连都会被跳过。

- **这是从代码推出来的，尚未在真机或日志里证实。** 实施的第一步：拉现场日志，统计出现 `netmon: network unavailable`（WARN 级，release 日志可见）之后、本次连接内再也没有 `netmon: reconnect` 的会话；再在真机上开关飞行模式复现。
- 修法二选一，由实施者在证实后定：(a) Android 在"之前上报过 unavailable"之后的第一个 `onAvailable` 改发 `available`；(b) `netCoordinator` 在 down 状态收到 `changed` 时视为恢复。(b) 改在 Go 一处，对所有来源都生效，倾向 (b)；但要确认 iOS 和桌面没有"down 状态下仍发 changed、实际却没恢复"的情况。
- 独立分支，先于 F2 合并。§5.2 第 1 层依赖它。

## 6. 交付拆分与顺序

1. **Android 状态卡死**（§5.5）：独立小分支。
2. **402 误报**（§4.4 前半）：独立小分支。纯 k2 改动，桌面立刻受益。
3. **候选名单 + 契约 + 静态 provider**（§4.1–4.3）。
4. **判定 + 等网 + 新错误码 + UI 原因文案**（§4.4 后半、§5）。

5. **iOS 原因透传**（§5.1 iOS 段）：NE + K2Plugin 原生改动，随下一个 App Store 版本发布。不阻塞 1–4。

3 和 4 可以同一个分支，但 4 必须和 3 一起发布：只有 3 没有 4，断网时会把备选划光并报 503，正好落入反目标。

发布：k2 改动随 app 版本发布；webapp 改动要推 `webapp/x.y.z` tag 才能到达已安装客户端（根 CLAUDE.md "Web OTA"）。**新 webapp 配老引擎**时，备选字段被忽略，行为不变，所以 webapp 可以先发。

## 7. 验证

- **单元**：把判定写成纯函数，输入为（网卡状态，被动证据时间戳，主动检查结果，当前时间），输出四种情况之一，所有组合全覆盖。静态 provider 的排除、耗尽、Reset。§5.3 每条规则用假时钟和假检查覆盖，包括"Phase-B 途中断网 → 规则 2 → Reset → 等网 → 恢复 → 先 Phase-A"整条链。每个关键测试做变异验证（改坏实现后测试必须变红）。
- **并发**：`go test -race`；`go test ./lint/`（k2 并发 AST 规则）。
- **PVE netem**（复用 `f1lab.sh`，在桌面 daemon 路径上跑）：
  1. 首选节点黑洞 → 无感切换到备选，期间不出现 disconnected；
  2. 所有候选黑洞 → 最终 503，绝不出现 402；
  3. 保持网卡、切断所有外网 → reconnecting(106)，候选不被划掉；恢复外网后回到原节点；
  4. 网卡下线再上线（Linux 上 `ip link set down/up`） → 桌面端经第 3 层判为情况 2（桌面没有第 1 层），恢复后自动连上；
  5. 把检查地址 DNAT 到一个返回 302 的服务 → reconnecting(107)；
  6. 第 1 种场景跑到一半再切断外网 → 规则 2 生效，不报 503。
- **真机**：iOS、Android 各一台，Wi-Fi 下拔掉路由器 WAN 再插回；开关飞行模式。期望：显示重新连接中加正确原因，恢复后自动连上，全程不断开。Windows / macOS 各跑一次"拔网线"和"首选节点黑洞"。
- **日志**：每次判定一行 `DIAG: netcheck`，字段包括 result（1–4）、evidence（iface/passive/active）、各检查地址的结果和耗时、触发点（pre-phaseB / pre-exhaust / wait-tick / net-event）。pre-exhaust 那次判定总是执行主动检查、不采信被动证据（终审修正：被动证据可能来自断网之前）。每次切换一行 `DIAG: outbound-failover`，含 from/to（截断 URL）。上线后据此统计。

## 8. 留给实施者决定的

- ClientConfig 字段名和位置（§4.2）；组合 provider 的具体形状（§4.3）。
- 用哪个字段识别"同一台物理机器"（§4.1），要求稳定唯一。
- 被动证据时间戳放在哪一层，以及怎样区分 wire 拨号（§5.2）。
- 106 / 107 的最终取值（§4.4），前提是两端都没被占用。
- 手动 / 无备选 / 情况 4 时的 UI 文案（§5.3 规则 5）。
- Android 修法 (a) 还是 (b)（§5.5）。
