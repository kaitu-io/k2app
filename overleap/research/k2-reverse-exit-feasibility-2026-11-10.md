*调查日 2026-11-10。由 Claude 的子任务只读阅读 k2app 仓库的代码完成，没有运行测试或构建，原文未改。“我的判断”部分不是核实过的事实。*

# k2v5“反向出口”可行性调查（只读，未改任何文件）

路径均相对 `/Users/david/projects/kaitu-io/k2app/k2/`。

## 已核实的事实

**1. 谁能开流**
- 没有 HTTP/3 请求语义、CONNECT 或 MASQUE。QUIC 流是自定义帧：`[varint 0x4B32][type:1][addr_len][addr]`，TCP 应答 `0x00` 或错误（`wire/stream.go:16-33, 83, 137`）。
- 约定上只有客户端开代理流：`QUICClient.DialTCP` 调 `OpenStreamSync`（`wire/quic.go:694-720`），服务端 `handleConn` 里 `AcceptStream`，再由 `handleStream` 按类型分发 Meta/TCP/UDP/Overflow/Echo（`wire/quic.go:1476, 1507-1560`）。
- 传输层不禁止服务端开流，QUIC 上已有先例：UDP overflow 流由服务端开（`wire/quic.go:97-108`），客户端 `acceptOverflowStreams` 接收，但只认 `StreamTypeUDPOverflow`，其他类型丢弃（`wire/quic.go:1024-1062`）。
- TCP-WS 侧：smux 分叉双向都能 `OpenStream`（`third_party/smux/session.go:115-117, 131, 189`），但客户端没有任何 `AcceptStream` 循环，只有服务端有（`wire/tcpws.go:1009`）。服务端开的流会堆在 1024 的 backlog 里，满了阻塞 `recvLoop`（`session.go:16, 500-508`）。
- UDP：datagram 帧 `[session_id:4][addr_len][addr][payload]`，流式 UDP 再加长度前缀（`wire/udp.go:23-37, 133-137`）。

**2. k2s 出站**
- 入口 `acceptTCPLoop` → `ProxyHandler.HandleTCP`（`server/server.go:620-648`）。
- 拨号写死：TCP 是 `net.DialTimeout("tcp4", …)`（`server/handler.go:450`），UDP 是 `net.ListenUDP("udp4", nil)`（`handler.go:538`）。`ProxyHandler` 结构里没有 dialer 接口或出站抽象（`handler.go:303-332`）。
- 没有链式、上游或节点间转发。唯一的 relay 是按内层 SNI 把短 HTTP 请求反代到 Center（`server/control_plane.go:32-46`，`server/server.go:927`）。

**3. 客户端侧服务入站**
- `k2p.Listen` 就是 Home 模式：ICE 打洞后在 Home 端起 QUIC 监听，返回 `wire.Listener`（`k2p/home.go:21, 187-190`）。
- 它没有生产调用方，只有 `test/k2p_e2e_test.go:68` 用到；`cmd/` 下只有 k2、k2s、k2r。
- 已有现成工厂把既有 `quic.Conn` 包成任一角色：`NewQUICServerFromConn` 和 `NewQUICClientFromConn`（`wire/quic_p2p.go:17, 49`）。
- 出口处理器就是 `server.ProxyHandler`（HandleTCP/HandleUDP、保留地址拦截、BT 拦截、按设备计量），可以直接接在任何 `wire.Listener` 后面。

**4. 每流元数据**
- 身份是连接级的，不是流级的。元数据流 0x10 带 udid、token、mode、version、cookie（`wire/stream.go:252-264`）。
- Center 校验后返回 UserID、UDID、ServiceExpiredAt（`wire/auth_remote.go:74-88`）；udid 写入 `deviceIDHolder`，各流通过 `DeviceID()` 读取（`wire/identity.go:25-42`，`server/device_traffic.go:67-76`）。
- 目的地是流头里的 `host:port` 字符串。TUN 模式默认是 IP，只有嗅到 SNI 并触发改路由或 IPv6 改写时才是域名（`core/tunnel.go:338-351, 387-392`）。
- 节点另外能在 53 端口 DNS 路径看到域名，并以 INFO 记录（`server/handler.go:821-846`）。`HandleTCP` 也以 INFO 记录 addr（`handler.go:426-430`），这和“不记录访问网站”的承诺冲突。

## 我的判断

**5. 最小改动**
- **(a) k2s，约 300–500 行加测试**
  - wire：元数据 `mode="box"` 通过校验后，把该连接登记进 `boxRegistry[boxID]`，并包成 Dialer。不能直接用 `NewQUICClientFromConn`：它会再起一套 `AcceptStream` 和 `ReceiveDatagram`，与 `handleConn` 抢读（`wire/quic.go:1476, 1757, 2104`）。
  - server：给 `ProxyHandler` 加出站接口，把 `handler.go:450` 和 `:538` 换成“按 guest 查 Box，再 `box.DialTCP`”。
  - 新出口路径要过 admission gate 的 AST 测试。
- **(b) Box 程序，约 200–400 行**：新建 `cmd/k2box`，复用 `wire` 的拨号（ECH、指纹、元数据）和 `server.ProxyHandler`。前提是 wire 给 `QUICClient` 开一个钩子，把服务端开的 TCP/UDP 流交给 `wire.Listener`。
- **(c) 客户端可以不改**：Box 的选择放在 Center，由鉴权 verdict 多返回一个 `exitBox` 字段（`auth_remote.go:74`）。用内层 SNI（`wire/ech.go:72`）带 Box ID 也可行，但 QUIC 路径要另接线。
- guest 身份要在节点开给 Box 的流头里加字段，否则 Box 无法按 guest 记录和限速。

**6. 风险**
- **Box 鉴权**：现有 udid+token+Center 校验可复用，但要有独立的 mode，且 Box 连接不得获得正常出口权。
- **串户**：映射必须来自服务端 verdict，不能信客户端自报。
- **流控**：所有 guest 共用一条 Box 连接。连接窗口 20 MB、单流 8 MB、并发流上限 256（`wire/quic_limits_desktop.go`，`wire/quic.go:238-250`）。两三个慢 guest 就能占满连接窗口，拖住其他人。
- **TCP-WS 回退**：TCP 叠 TCP，有真实的队头阻塞。中国境内 Box 的 UDP 常被限速，很可能长期落在这条路径上。
- **拥塞控制方向反了**：下载流量变成 Box 上行，走的是客户端的 `NewCCSenderGFWMode`（`quic.go:622`），在家宽上行的表现没有验证过。
- **UDP**：双层 datagram 的 MTU 问题（`InitialPacketSize` 1400）；full-cone 状态要搬到 Box。
- **保活**：QUIC keepalive 10 秒，空闲超时客户端 30 秒、服务端 90 秒（`quic.go:238-262`）；smux 是 10 秒和 30 秒（`tcpws.go:71-79`）。过 NAT 够用。
- **Box 断线**：在途流全部断，没有迁移。guest 的 App 看到的是连接重置，而不是节点故障；Box 掉线时要防止静默退回机房出口。
- **域名可见性**：Box 多数时候只能拿到 IP，除非 guest 的 DNS 也经 Box 解析。

**结论：中等，约 2–4 周，不是重构。** 帧格式、鉴权、出口处理器和连接包装都现成，缺的是 k2s 的出站抽象、Box 注册表、客户端侧收流和 guest 标识。几天内只能做出仅 QUIC、仅 TCP 的演示。

**建议先做的原型**：仅 QUIC、仅 TCP 的反向流，guest → 节点 → 真实中国家宽 Box。测三样：
1. 多 guest 并发下载时的吞吐和互相拖累程度（连接窗口和 CC 方向）。
2. 强制走 TCP-WS 时的表现。
3. 拔掉 Box 后 guest 端的恢复行为。
