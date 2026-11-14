*调研日 2026-11-10。由 Claude 的调研子任务通过网页搜索完成，原文未改。标【判断】的是推断，标【未核实】的没有查到一手来源；Wyse 3040（Z8350）上没有任何公开实测。*

# 组网栈选型调研(2026-11-10)

**结论:首选 A 的“增强版”,即盒子用内核 WireGuard、App 用 wireguard-go 加自定义 Bind、协调与中继自建;次选 B 的 Tailscale 客户端栈加自写控制面。** Z8350 上没有任何公开实测,最终要靠真机数据定。未标注的为有来源事实,【判断】为我的推断,【未核实】为本次没查到一手来源。

## 1. 许可证

- **A(WireGuard 官方库)**:可闭源商用,Windows 有约束。
  - wireguard-apple(WireGuardKit)为 MIT:https://github.com/WireGuard/wireguard-apple
  - wireguard-android 为 Apache-2.0:https://github.com/WireGuard/wireguard-android
  - wireguard-nt 源码是 GPLv2,预编译 DLL 另有较宽松许可:https://git.zx2c4.com/wireguard-nt/plain/README.md
  - 该 DLL 许可只允许原样分发、只经 `wireguard.h` 调用、不得修改:https://git.zx2c4.com/wireguard-nt/plain/prebuilt-binaries-license.txt
  - wireguard-go 为 MIT【未核实】。
- **B(Tailscale)**:开源部分可用,但三大平台 GUI 和控制面要自己做。
  - 主仓库和 libtailscale 为 BSD-3:https://pkg.go.dev/tailscale.com@v1.78.1 、https://github.com/tailscale/libtailscale
  - Windows/macOS/iOS 的 GUI 和控制服务器闭源:https://tailscale.com/blog/opensource
  - 官方商标政策原文没查到;第三方分支的通行做法是保留 BSD、声明无关联、不用其名【未核实】。
  - Headscale 自述范围是“单个 tailnet,适合个人或小型开源组织”:https://raw.githubusercontent.com/juanfont/headscale/main/README.md
  - 其 FAQ 称不是企业软件、性能不是关注点,推荐 SQLite,但不禁止商用:https://headscale.net/stable/about/faq/
  - 有 issue 报告旧 alpha 版约 500 节点后拒绝连接:https://redirect.github.com/juanfont/headscale/issues/1656
  - Headscale 自身许可证(BSD-3)【未核实】。
- **C(NetBird)**:客户端可闭源嵌入,服务端改了就要开源。
  - 客户端仍为 BSD-3;`management/`、`signal/`、`relay/` 自 v0.53.0 起改为 AGPLv3:https://forum.netbird.io/t/agpl-license-update/153 、https://newreleases.io/project/github/netbirdio/netbird/release/v0.53.0
  - 【判断】把自有账号体系接进 management 属于修改,对外提供服务就触发 AGPL 开源义务;自写兼容的管理端可以避开。
- **D 类**:
  - ZeroTier 1.16 起核心和服务为 MPL,控制器为非自由商业许可:https://docs.zerotier.com/changelog/2025/08/21/zt1-v1.16.0
  - Nebula 为 MIT:https://awesome.ecosyste.ms/projects/github.com%2Fslackhq%2Fnebula
  - EasyTier 官方文档写 Apache-2.0,有分支写 LGPL,存在冲突:https://easytier.top/en/guide/introduction.html
  - sing-box(GPLv3)、Hysteria2(MIT)、iroh(MIT/Apache)、innernet、Firezone、Netmaker 的许可证【未核实】。

## 2. NAT 穿透

- **Tailscale** 公布的直连率是“典型条件下远高于 90%”,这是内部指标,不是我们的人群:https://tailscale.com/blog/nat-traversal-improvements-pt-1
  - 同文说 CGNAT 常是对称映射加短超时,两台不同蜂窝网的手机多半走中继。
  - 一侧对称 NAT 可用生日悖论探测(256 端口对 1024 次探测约 98%),两侧对称基本无解;它还用 UPnP/NAT-PMP/PCP:https://tailscale.com/blog/how-nat-traversal-works
  - DERP 跑在 HTTP 上(同一出处);是否固定 TCP/443【未核实】。
  - Peer Relay 于 2026-02 正式发布,优先于 DERP:https://tailscale.com/blog/peer-relays-ga
- **libp2p DCUtR** 的条件成功率是 70%±7.1%(440 万次尝试,TCP 与 QUIC 无显著差异):https://arxiv.org/abs/2604.12484
- **NetBird** 用 pion/ICE,自研中继同时试 QUIC 和 WebSocket(可放在 443 反代后);双侧对称 NAT 或 UDP 被封时走中继:https://docs.netbird.io/about-netbird/understanding-nat-and-connectivity 、https://docs.netbird.io/selfhosted/selfhosted-guide
  - 没找到它公布的直连率。
- **iroh** 的直连率说法是二手且互相矛盾的(70%、90%、95–99%):https://pinggy.io/blog/iroh_1_0_dial_keys_not_ips/
- 【判断】我们的组合是“家宽主机 + 手机访客”,直连率会低于 Tailscale 的数字。最大的杠杆在盒子侧:它在家庭内网,可以做 UPnP/PCP 端口映射和 IPv6。

## 3. 低端 CPU 吞吐

- **内核 WireGuard**(OpenWrt 社区 wg-bench,本机 netns 加 iperf3,数字偏保守):https://forum.openwrt.org/t/a-wireguard-comparison-db/187586/1

  | CPU | 吞吐 |
  |---|---|
  | Celeron J1900(4 核 2.0 GHz) | 699 Mbps |
  | Atom E3825(2 核 1.33 GHz) | 259 Mbps |
  | Celeron N2930 | 762 Mbps |
  | Celeron J4125 | 1.76 Gbps |
  | 树莓派 4 | 881–1020 Mbps |

- 【判断】Z8350 与 J1900 同代但频率更低,内核 WireGuard 大约 350–550 Mbps,能达到 100–300 Mbps 目标。
- **Tailscale(用户态 wireguard-go)**:
  - 厂商称在 i5-12400 和 AWS c6i 上超过内核实现,超过 10 Gb/s:https://tailscale.com/blog/more-throughput
  - UDP 卸载需要 Linux 6.2+ 和 v1.54+,出口节点流量会吃满单核:https://tailscale.com/docs/reference/best-practices/performance.md
  - 低端机只有一条用户自述:树莓派 4 裸速 900+,经 Tailscale 约 180 Mbps:https://red.127.pp.ua/r/Tailscale/comments/1fd9l2x/cheapest_travel_router_solution/lmihzfe
  - 2026-09 的优化约提升 5%,出口节点多队列“计划中”:https://tailscale.com/blog/making-tailscale-faster
- **QUIC/MASQUE**:没找到量化对比,只有“比内核 WireGuard 更费 CPU”的定性说法:https://habr.com/ru/article/972172
- 【判断】在 Z8350 上,用户态方案(Tailscale、Hysteria2、sing-box)大概率落在 100–200 Mbps,处于目标下沿。

## 4. 出口、计量和日志

- 【判断】内核 WireGuard 下每个访客一个 /32 隧道地址,全部用标准工具:
  - 计量:`wg show transfer` 自带每 peer 字节数,或用 nftables 计数。
  - 限速:tc 或 nft limit 按访客 IP。
  - 目的地日志:conntrack 事件记 IP,盒上 DNS 解析器记域名。
- Tailscale 和 NetBird 都有出口节点功能,计量同样靠隧道 IP 加 nftables。NetBird 在 Linux 上默认用内核 WireGuard:https://serverspace.io/about/blog/how-to-deploy-netbird-on-a-linux-server/
- Nebula 只有 `unsafe_routes`,没有明确的出口节点功能。
- 记录目的地的能力要和对外的隐私说法对齐。

## 5. 自有品牌客户端

- **A**:四个平台都有官方嵌入库,见 https://www.wireguard.com/embedding/
  - 【判断】要做穿透和 TCP 中继,App 端应统一用 wireguard-go 加自定义 `conn.Bind`,这正是 Tailscale 的做法;Windows 用 wintun,不用 wireguard-nt。
- **B**:libtailscale/tsnet 是纯用户态进程内网络,不是系统级 VPN。【判断】全局隧道要自己围绕 tailscaled 做 Network Extension 和三大平台 GUI。
- **iOS 约束**:隧道扩展内存上限据 Apple 论坛反馈约 50 MB(非官方文档,历史上是 15 MB):https://developer.apple.com/forums/thread/73148 。Tailscale 曾因此被系统静默杀进程:https://tailscale.com/blog/go-linker
- **电池**:没查到数据。【判断】WireGuard 空闲时静默,主要耗电来自 NAT 保活间隔。

## 6. 规模与运维

【判断】我们是“一访客对一盒”的配对,不是网状网。

- A 的控制面只需下发一对公钥和端点,无状态、可水平扩展,数万盒子没有网络图计算问题。
- Headscale 的全网图计算和单 tailnet 设计与此不匹配,只能当原型用。
- NetBird 的 management 在数万节点下的表现没有公开数据。

## 7. 工作量(2–3 人)【判断】

| 方案 | 到“盒↔App 出口 + 穿透 + 中继”可用 | 备注 |
|---|---|---|
| A 增强版 | 3–4 个月,四端打磨再加 2–3 个月 | 可参考 NetBird 客户端 BSD-3 代码 |
| B + Headscale 原型 | 2–4 周 | 只够验证 |
| B 生产化 | 5–8 个月 | 自写控制面和四端品牌 App |
| C 原样自托管 | 1–2 个月 | 接自有账号会触发 AGPL 或要重写管理端 |

## 推荐

1. **A 增强版**:盒子用内核 WireGuard 加一个小守护进程(STUN、端口映射、ICE 式打洞);App 用 wireguard-go 加自定义 Bind;自建无状态协调服务和 UDP + WebSocket/443 中继。
   - 主要风险:穿透的长尾问题(对称 NAT、漫游、休眠唤醒)全由我们自己承担,直连率起步会低于 Tailscale。
2. **B(Tailscale 客户端栈 + 自写控制面)**:穿透最成熟。
   - 主要风险:Z8350 上用户态吞吐可能不达标;协议由对方单方面演进;三大平台 GUI 要自己做;商标边界未核实。

## 先做的原型

1. 在真机 Wyse 3040 上测内核 WireGuard、tailscaled、NetBird 三者作为出口的吞吐和单核占用,直连和中继各测一次。
2. 取 20–30 组真实的“家宽 × 手机/酒店/公司网”组合,用 Tailscale + Headscale 和 NetBird 各跑一遍,统计直连率,作为自研的基线。

## 仍不确定

- Z8350 的实测吞吐,以及内核不走 AVX 时的 ChaCha20 表现。
- 我们这类人群的真实直连率。
- Tailscale 的商标条款和 DERP 端口细节。
- NetBird 移动端仓库的许可证。
- 上面标注【未核实】的各 D 类项目许可证。
