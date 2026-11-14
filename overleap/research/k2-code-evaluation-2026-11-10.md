*评估日 2026-11-10。由 Claude 的子任务只读阅读 k2app 仓库的代码和文档完成，没有运行测试或构建，原文未改。“判断”部分不是核实过的事实。*

以下结论只基于读代码和文档，没有跑任何测试或构建。

## 结论

**不建议拿 k2 当盒子↔App 的数据面。** 新产品最缺的四块（家庭端入口、可用的信令和中继、按用户限速、流级记录）在 k2 里都不存在或未完成。k2 最成熟的部分是抗封锁，新产品用不上。

## 1. k2p 和 signal（已核实）

- **角色**：Home 用 `k2p.Listen`（`k2/k2p/home.go:21`），Away 用 `k2p.Dial`（`k2/k2p/away.go:24`）。
- **穿透**：用的是 pion/ice v4，标准 ICE 连通性检查加 trickle 候选，不是自研打洞。
  - 只开 UDP4，没有 IPv6，也没有 TCP（`k2/k2p/ice.go:88`）。
  - STUN 列表硬编码，含 miwifi 和 bilibili（`ice.go:55-60`）。
  - 默认 TURN 地址是 `turn.kaitu.io`（`k2/k2p/config.go:10`）。
- **中继**：靠 TURN 候选。仓库里没有 TURN 服务端的生产代码，`turn.NewServer` 只出现在 `k2/signal/turn_test.go:56`。
- **连上之后**：在 ICE 连接上跑 QUIC（kuic），再跑 k2v5 流代理。Away 端的拥塞控制写死为 GFW 模式（`k2/wire/quic_p2p.go:20`）。
- **鉴权缺口**：
  - Away 的 WS 和 lookup 不验任何身份，lookup 会给任何人发 TURN 凭据（`k2/signal/handler.go:110-134`、`184-209`）。
  - claim 先到先得，注册表只在内存里（`k2/signal/store.go:28`）。
  - Pin 为空时 `InsecureSkipVerify` 且不做任何校验（`k2/k2p/auth.go:108-121`）。
  - `SecretHash` 为空时完全不鉴权（`home.go:174`）；设了也只验第一条 QUIC 连接，之后同一 listener 继续接受连接而不再验（`home.go:187`、`quic_p2p.go:49-55`）。
  - 数据面 validator 为 nil 时放行，流量记进空 udid 桶，所以没有按用户计量（`k2/wire/metadata_auth.go:67-71`、`k2/wire/admission.go:104-106`）。
- **完成度**：
  - k2p 1181 行、signal 702 行，测试文件 5 个和 4 个，TODO 为 0。
  - e2e 测试只在本机回环上跑，断言的是 `p2p-lan`（`k2/test/k2p_e2e_test.go:107`）。
  - `k2p.Listen` 和 `signal.NewHandler` 在生产代码里没有调用方，只有测试用；`cmd/` 下没有 k2p 入口。
  - 客户端一侧接进了 engine（`k2/engine/engine.go:519`、`k2/core/outbound_k2p.go`）。
  - 断线后是整套重拨，没有 ICE restart。
- **成功率**：仓库里没有实测数据。`docs/superpowers/specs/2026-10-16-overleap-home-exit-network-design.md:14` 的 60–85% 是引文献的估计，原文写明要试点实测。

## 2. k2r 和 gateway（已核实）

- **功能**：用 nftables 或 iptables 的 TPROXY 截获局域网的 TCP 和 UDP，交给 engine，经 k2v5 隧道出去。仅 Linux，支持 systemd 和 OpenWrt procd（`k2/gateway/CLAUDE.md`、`k2/cmd/k2r/CLAUDE.md`）。
- **成熟度**：5417 行，28 个测试文件，Docker UAT 有 56 个检查点，在 OpenWrt 虚拟机上验证过。文档里记了大量真实踩坑，是三块里最扎实的。
- **已知限制**：没有 kill switch，隧道断了局域网直接走直连。
- **依赖**：会拉进 engine、core、provider、rule、subscription、webui、wire、k2p 整套客户端栈（`go list -deps`）。控制密钥以 Center 的 `/api/subs` 为准。
- **能否直接用**：不能。它的出口必须是 k2v5、k2subs 或 k2p 的隧道。

## 3. k2s 和 wire（已核实）

- **数据路径**：服务端是用户态 Go 的四层代理，不用 TUN，也不用内核 NAT（`k2/CLAUDE.md` Architecture）。客户端用 sing-tun 加 gVisor。主通道是 QUIC/H3（kuic），备用是 TCP-WebSocket 加 smux。
- **伪装机制**：
  - ECH 配置伪造和轮换，非 ECH 的 SNI 反向代理到真实站点，TLS 记录填充（`k2/server/CLAUDE.md`）。
  - Chrome-146 的 QUIC 指纹（`k2/wire/quic_fingerprint.go`）。
  - 屏蔽中国 CA，内嵌一张伪装成 DigiCert 的自有 CA（`k2/wire/ca.go`）。
  - 控制面中继默认指向 `k2.52j.me`。
  - 端口跳跃已经删掉。
- **可分离性**：`wire.Dialer` 和 `Listener` 接口干净，但实现和伪装缠在一起。wire 的 16328 行里，`transport.go`、`race.go`、`ech*.go`、`cookie.go`、`session.go` 等都是抗封锁或 Center 鉴权。kuic 整体换成了 utls（`third_party/kuic/MAINTAINING.md`），41969 行，每次跟上游都要重新核对。
- **拥塞控制**：k2cc 是 PCC Vivace 加 BBR 式带宽锚定，2005 行，在 `k2/wire/k2cc/`，只依赖 kuic 的 `congestion` 接口，可以单独搬走。参数为高丢包跨境线路调的：α=4 容忍 25% 丢包，速率下限 5 Mbps，冷启动锚点 30 Mbps（`k2cc.go:43-48`）。仓库里没有 brutal 或 bbr 的独立实现。
- **基准**：没找到吞吐基准或 CPU profile。`k2/docs/todos/p2-performance.md` 里 Speedtest 和 Performance tuning 两项都没勾。

## 4. 计量、配额、限速、目的地日志（已核实）

- **k2s 计量**：按 UDID 加会话累计字节，在连接结束时入账，批量上报 Center（`k2/server/device_traffic.go`、`device_traffic_reporter.go`）。
- **sidecar**：量的是整机网卡流量，超额时暂停容器。这是节点级的，不是用户级（`docker/sidecar/CLAUDE.md`）。
- **限速**：在 server、wire、config、cmd/k2s 里没搜到任何限速器。
- **拒绝流量**：
  - BT 只认明文握手，加密 BT 检不出（`k2/server/bt_block.go`，server 文档写明了这个缺口）。
  - 屏蔽保留 IP（`checkAddr`）。
  - 没看到端口黑名单。
- **目的地日志**：在 INFO 级别明文记录，不加密，也没有结构化的流记录。
  - 每条 TCP 的目的地址：`k2/server/handler.go:426-430`。
  - 每个 UDP 会话的目的地址：`handler.go:488-493`。
  - DNS 查询的域名：`handler.go:821`、`831`、`846`。

## 5. 耦合与规模（已核实）

非测试 Go 行数：

| 包 | 行数 |
|---|---|
| third_party/kuic | 41969 |
| wire | 16328 |
| engine | 8315 |
| gateway | 5417 |
| server | 2902 |
| k2p | 1181 |
| signal | 702 |

- **Go 版本**：1.26（toolchain 1.26.10）。
- **主要依赖**：kuic 和 smux 走本地 `replace`，另有 metacubex/utls、sing-tun、pion/ice、pion/turn、google/nftables、k2-rules。
- **跨仓库引用**：`replace` 不传递，外部仓库要用 k2 就得自己再 replace kuic 和 smux（`k2app/CLAUDE.md`）。
- **Center 耦合**：k2s 的鉴权和计量上报、k2r 的控制密钥和订阅都指向 Center。k2p 和 signal 反而不依赖 Center。
- **订阅**：`k2subs://` 只在桌面 daemon 和 gateway 里用。

## 6. 我的判断

**(a) 值得近乎原样拿走的**
- gateway 的 TPROXY 部分：`listener.go`、`intercept*.go` 和 UDP 流表，连同文档里的踩坑记录。
- `sniff/`（1223 行，识别 TLS、QUIC、HTTP、BT、STUN）。
- `device_traffic*.go` 的幂等批量上报做法。
- `signal/turn.go` 的 TURN REST 凭据生成（82 行）。
- 全锥 UDP 的语义说明。

**(b) 要大改的**
- k2p 和 signal 本质上是个可运行的原型，约 1900 行。要补的有：信令鉴权、票据、持久化、IPv6、ICE restart、家庭端入口，还要自建 TURN。
- k2cc 的参数要按家宽上行重调，否则会挤占房主自己的网络。
- gateway 要从 engine 和 subscription 上解下来。

**(c) 用不上的包袱**
- ECH、反代伪装、TLS 填充、Chrome 指纹、TCP-WS 回退与通道选择状态机、cookie 和重验证、控制面中继、CA 屏蔽、规则引擎与 krs 规则包、k2subs、probe。
- utls 版的 kuic 分叉（4.2 万行，要长期维护）。

**工作量对比（粗估）**
- **复用 k2 数据面**：要背上 wire + kuic + smux 约 6 万行，其中大半是用不上的伪装逻辑。还得新写限速、端口策略、加密流日志、家庭端入口、信令和 TURN。用户态 QUIC 在 Atom x5 上的吞吐，仓库里没有任何数据可以佐证。
- **内核 WireGuard 加薄协调层**：
  - 计量、限速、端口拒绝用 nftables 和 tc 按 peer 做，都是内核现成能力。
  - 协调层（STUN 端点发现、交换端点、中继兜底）规模和现有 k2p + signal 相当，约 2000–3000 行。
  - 客户端有官方的 wireguard-go 和 wireguard-apple。
  - 代价：流的域名记录要另做（conntrack 加 SNI 或 DNS 旁路）；WireGuard 没有内建中继，要自己做 UDP 中继，或者直接用 Tailscale 的 DERP 和 magicsock。
  - 后者工作量大约是前者的一半到三分之二，长期维护面小一个量级。

**一个要先确认的前提**：如果盒子的流量要穿过中国的防火墙（会员在大陆），WireGuard 的指纹会被识别，那一段仍然需要 k2v5 入口。这一项决定“完全不用 k2”是否成立。
