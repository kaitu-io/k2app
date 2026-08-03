# UDP 游戏体验优化 — 设计

日期：2026-08-04
状态：已批准（方案 A：SP1 根治先行，SP2/SP3 并行，SP4 收尾，一次推进到底）
背景工单：用户反馈「别的工具能玩游戏、网页打开游戏也没问题，用我们的就有问题」（无设备日志；根因由代码调查闭环）

## 1. 问题与根因

浏览器游戏走 TCP+HTTPS（有 SNI，嗅探与域名规则链路健全）所以正常；游戏客户端是裸 IP + 大量 UDP，连续踩中以下缺口。全部为代码事实（k2 submodule 指针为 2026-08-04 main）：

### P0（正确性 / NAT 级，SP1 修）

1. **一个源端口只能与一个目的地址通信**。客户端 UDP flow 按源端口聚合（sing udpnat2 cache key 仅 source），但 `core/tunnel.go:727` 的 `pipePacket` 丢弃了每包真实目的地址，`:731` 无条件写首包目的；wire 层 `quicUDPConn` 把目的钉死在 `targetAddr`（`wire/quic.go:791`）。游戏客户端标准姿势是一个 socket 同时与主服务器 / 对战服务器 / STUN 通信——第二个目的起的包会被发往第一个目的。SOCKS5 proxy provider 反而是按 dest 建 session 的（`provider/proxy.go:381`），TUN 与 proxy 路径语义不一致。
2. **服务端出口为对称型 NAT**。k2s 对每 session `net.DialUDP("udp4", nil, target)`（`server/handler.go:334`）：connected socket、随机端口、钉死单目的、拒收第三方来源 → Symmetric NAT / NAT Type Strict。叠加 STUN 被硬编码保持走代理（`core/tunnel.go:386-391`，工单 #3051），游戏 ICE 必然拿到最差 NAT 判定。
3. **拥塞时上行静默丢包且谎报成功**。`maxDatagramInflight = 8`（`wire/quic.go:55`；`k2/CLAUDE.md`、`wire/CLAUDE.md` 写 32 是过期文档），超限直接丢并 `return len(b), nil`（`quic.go:2044-2050`）；下行超限却降级为可靠 overflow stream（`quic.go:2192-2207`）——方向不对称。`txDrops` 仅在连接关闭时打一条 INFO，WireStats / heartbeat 均无丢包字段——线上不可见。

### P1（规则表达力，SP2 修）

4. **端口维度是死字段**：`rule/conn.go:15 ConnMeta.Port` 定义了但 `routeConn` 不填、`matchMeta` 不读、`MatchConfig` 未暴露。六个对标工具（mihomo/sing-box/Xray/Surge/Shadowrocket/QX）全部支持 DST-PORT。
5. **协议判定不可规则化**：嗅探覆盖面行业领先（TLS/HTTP/QUIC/BT/STUN/DTLS/uTP），但结果全部消费在硬编码策略里（BT→强制直连、STUN/DTLS→强制代理）。sing-box 把 `protocol` 做成普通匹配维度。
6. **tmp pin 容量满整表清空**（`rule/engine.go:479-486`，cap 4096 非 LRU），裸 IP 流量命中率受损。

### P1（规则内容，SP3 修）

7. 无 `category-games` 分类包；游戏厂商 ASN 集合缺位（ipverse 管道已存在，现仅用于 tencent AS132203）。
8. 内容债：preset 表 14 国 vs 数据源 18 国漂移（tm/kz/uz 进不了 preset）。

### 已知但本轮明确不改

- `maxDatagramInflight=8`、UDP 空闲超时 60s、`connSem`/`readCh` 各容量：**只加观测不调参**——无线上丢包数据前调参是盲调；遥测落地后按数据决策。
- TCP-WS 回落的 UDP 队头阻塞（smux 单 stream + writeMu 串行化，`tcpws.go:1195-1235`）：回落是降级路径，先修主路径。
- iOS/Android per-connection app 归因、fake-ip：维持既有结论（tmp pin 方案 + LRU 化足够；iOS NE 50MB 内存宪法约束下不引入 fake-ip 表）。
- 规则包签名 / 内容寻址 tarball、未命中遥测 Phase 2：既有 plan 存续，不并入本轮。

## 2. 目标 / 非目标

**目标**
- 游戏客户端经隧道可正常联机：多目的 UDP 正确送达、NAT 检测结果为 Full Cone（EIM+EIF）、ICE 可用。
- 规则层能表达游戏流量：目的端口、嗅探协议、games 域名/ASN 集合。
- 游戏丢包线上可观测（heartbeat 携带 datagram 丢弃计数）。

**非目标**
- 不做游戏加速器产品形态（专线、多路径、延迟优化）。
- 不暴露用户级自定义规则编辑器（产品定位维持 4 预设 + App Bypass）。
- 不改隧道拥塞控制与队列参数。

## 3. SP1 — k2 隧道 UDP 语义修复（仓库：k2）

### 3.1 协议基础（零 breaking change 的依据）

wire UDP 帧 `SmuxUDPFrame`/`UDPFrame` 每帧携带 addr 字段（`wire/udp.go:63-70`）：上行客户端本就逐帧发目的地址（`quic.go:2036`），下行服务端 addr 留空（`quic.go:2186`）。协议已为 per-packet 目的地址留好位置，本子项目只改两端的**使用方式**，不改帧格式。

### 3.2 客户端：pipePacket 与 wire conn 变为 dest-aware

- `pipePacket`（`core/tunnel.go`）：上行 `local.ReadFrom` 保留返回的真实目的地址，`remote.WriteTo(buf, perPacketDest)`；下行把 `remote.ReadFrom` 返回的真实来源透传给 App（direct 路径已如此，wire 路径需 wire conn 配合）。
- `quicUDPConn.WriteTo`：addr 参数非 nil 时按包序列化该地址，nil 时回落 `targetAddr`（兼容既有调用点）。
- `quicUDPConn.ReadFrom`：下行帧 addr 非空时返回帧内真实来源，为空时维持 `targetAddr`（对老服务端行为不变）。
- TCP-WS `smux` UDP 路径同步同样语义（`tcpws.go` 帧本就带 addr）。
- direct 路径（`outbound_direct.go` unconnected socket）自动受益：多目的上行随 pipePacket 修复直接可用。
- 路由语义不变：flow 的 via 判定仍按首包目的（与 sing-box TUN UDP NAT 行为一致），本轮不做逐包重路由。

### 3.3 服务端：connected → unconnected，full-cone

- `server/handler.go HandleUDP`：`net.DialUDP` 改 `net.ListenUDP("udp4", nil)`（一 session 一恒定出口端口），上行逐帧解析 addr 后 `WriteTo`；下行接受任意来源，回程帧 addr 填真实来源 `ip:port`。
- 语义 = Endpoint-Independent Mapping + Endpoint-Independent Filtering（full-cone）。
- 防护维持现状：session 认证在隧道入口完成；出口 socket 生命周期仍受 60s 空闲收割与 `maxServerUDPSessions=4096` 约束；来源不限但只回传给绑定的 session。
- addr 解析失败的帧丢弃并计数（不 fail session）。

### 3.4 兼容矩阵（可灰度依据）

| 客户端 \ 服务端 | 老 k2s（connected） | 新 k2s（full-cone） |
|---|---|---|
| 老客户端（逐帧发目的，忽略下行 addr） | 现状 | 多目的可用（服务端逐帧 WriteTo）；下行 addr 被老客户端忽略，App 视角不变 |
| 新客户端 | 等同现状（服务端钉死首目的） | 完整 full-cone |

先发服务端（车队 canary → 批量），客户端随下一版本发布。

### 3.5 观测与文档

- `WireStats` 增加 `UDPTxDrops` / `UDPRxDrops`（累计计数），engine heartbeat DIAG 携带。
- 运行中丢弃采样日志（限频）替代"仅关闭时一条 INFO"。
- 更正 `k2/CLAUDE.md`、`k2/wire/CLAUDE.md` 的 `maxDatagramInflight` 过期值（32→8）。

### 3.6 测试

- TDD：wire 层多目的收发单测（新旧帧 addr 空/非空四象限）、服务端 unconnected socket 回包路由、pipePacket dest 透传。
- race tests 沿用 300s+ 超时约定。
- e2e：单 socket 双目的 echo（隧道内）+ 第三方来源回包（full-cone filtering）+ 新旧混布两向。
- 真机业务 smoke：桌面 + 一个移动端，真实游戏客户端 + NAT 类型检测（如 stunclient），canary 节点验证后再车队批量。

## 4. SP2 — k2 规则能力（仓库：k2，SP1 合入后开工）

### 4.1 端口维度

- `MatchConfig` 增 `port`（`[]string`，元素为 `"3478"` 或 `"27000-28000"`）；编译进 `RouteEntry.PortRanges []portRange`。
- `routeConn` 从 dest 填 `ConnMeta.Port`；`matchMeta` 增加端口比对（属 meta 维度，与既有 network/进程语义一致：组间 AND）。
- webapp `ClientConfig` TS 类型同步（SP4 消费）。

### 4.2 协议维度与 STUN 规则化

- `MatchConfig` 增 `protocol`（`stun|dtls|quic|bittorrent`），来源为既有嗅探结果；`ConnMeta` 增 `Protocol string`。
- `core/tunnel.go` 的硬编码策略改为「规则可覆盖的默认」：引擎无 protocol 规则命中时，行为与今日完全一致（BT→direct、STUN/DTLS→保持代理，#3051 回归测试必须继续通过）。
- 匹配时机：协议在嗅探后才可知，因此 protocol 规则仅参与连接期第二次判定（沿用现有 SNI 升级同款「先路由、嗅探后修正」管线），不引入新管线。

### 4.3 tmp pin LRU

- `SetTmpRule` 容量满从整表清空改为 LRU 逐出；cap 维持 4096（iOS NE 内存宪法：增量脏堆 <8KB 约束下不扩容）；reload 清空语义保留（路由表变更后旧 pin 不可信）。

## 5. SP3 — 游戏规则内容（仓库：k2-rules，与 SP2 并行）

- `sources.go` 新增 `games` 集合：v2fly `category-games`（域名）+ 游戏厂商 ASN（ipverse 管道，首批：Valve AS32590、Riot AS6507、Blizzard AS57976、Epic/Psyonix AS394699、Nintendo AS11282、Sony SNEI AS33353，编译期核对）→ 产出 `games.krs` 进 manifest。
- k2 侧 `rule/target.go` presets 注册 `games`；同时补 tm/kz/uz 三国 preset（数据源已有包）。
- 内容护栏：沿用 `validate-krs` 空包拒绝 + ruleCount 回归。

## 6. SP4 — webapp / 跨层接线（仓库：k2app）

YAGNI 收敛后本轮只做三件事：

1. `ClientConfig` TS 类型补 `port` / `protocol` match 字段（与 Go `MatchConfig` 对齐，桥接层 snake_case 约定不变）。
2. bypass 模式路由**不变更**——SP1 落地后游戏走隧道即为 full-cone，无需激进直连；`games` 集合本轮仅作为 classify/运营侧可用词汇与后续实验位，不上用户可见 UI。
3. integration-qa 回归：三平台连接 + App Bypass + 智能分流不回归；游戏场景真机 smoke 记录进发布信心评估（代码信心 / 业务信心分开打分）。

## 7. 交付与发布顺序

1. SP1 k2 分支 → review → 合入 → k2s 镜像 canary 一台 → NAT/多目的验证 → 车队批量（kaitu-node-ops 脚本）。
2. SP2 k2 分支（依赖 SP1 合入）与 SP3 k2-rules 分支并行 → 各自 review 合入；k2-rules 发日更 release。
3. SP4 k2app 分支：TS 类型 + submodule 指针推进 + 回归；随下一客户端版本发布。
4. 成功判据：隧道内 NAT 检测 Full Cone；单 socket 多目的 e2e 通过；真实游戏真机可联机；heartbeat 可见 UDP 丢弃计数。

## 8. 风险

- **服务端 unconnected socket 接受任意来源**：full-cone 的定义即如此，与行业工具一致；风险面为出口端口收到无关流量并透传给客户端 App——由 60s 收割、4096 session 上限与客户端 App 自身的来源校验约束。不引入额外过滤开关，保持语义纯粹。
- **老客户端 + 新服务端**：下行帧开始携带 addr，老客户端 `ReadFrom` 忽略之——已由代码确认（`quic.go:2020` 恒返回 resolved targetAddr），仍列入混布 e2e。
- **k2 是共享 submodule**：SP1/SP2 在 k2 独立仓 worktree 开发，k2app 仅在 SP4 推进指针，不在父仓改 k2/。
