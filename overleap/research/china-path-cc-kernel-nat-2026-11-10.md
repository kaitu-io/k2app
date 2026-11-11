*调研日 2026-11-10。由 Claude 的调研子任务通过网页搜索完成，原文未改。标“判断”的是推断，标“未证实”的没有查到一手来源。*

# 调研结论（2026-11-10）

要点：大陆路径不要让裸 WireGuard 过墙，改走「伪装入口节点 → 盒子主动拨出 WireGuard」；有损路径上 BBRv3 反而不如 v1；盒子用 6.12 LTS 即可，7.x 没查到必须的收益。以下标「判断」是我的推断，标「未证实」是没找到一手来源。

## 1. 裸 WireGuard 过墙
- **没找到针对 WireGuard 的严格测量论文。** 现有证据都是用户报告。
- **端口封锁报告（2022）：** 连续使用约一周后，对端 UDP 端口被封几天，只影响 IPv4。https://github.com/net4people/bbs/issues/117
- **"已完全封锁"的说法（2025-12）：** 单个用户评论，未证实。https://github.com/net4people/bbs/issues/418
- **运营商层面：** Hysteria 文档称中国用户反映 ISP 会封锁或限速持久 UDP 连接，通常只针对在用端口。https://v2.hysteria.network/docs/advanced/Port-Hopping/
- **GFW 对 UDP 的能力：** 全加密流量检测只针对 TCP（https://gfw.report/publications/usenixsecurity23/en/ ）；但 2024-04 起 GFW 解密 QUIC Initial 按 SNI 封锁（https://gfw.report/publications/usenixsecurity25/en/ ）。
- **判断：** WireGuard 握手格式固定、易被指纹识别，不能作为过墙传输。

## 2. 混淆方案
- **AmneziaWG：** 内核模块 GPLv2、树外构建（https://github.com/amnezia-vpn/amneziawg-linux-kernel-module ）；Go 版 MIT，文档已到 "AWG 3+"（https://github.com/amnezia-vpn/amneziawg-go ）。
  - 俄罗斯：DPIdetector 把它列入被封协议（https://www.hrw.org/report/2025/07/30/disrupted-throttled-and-blocked/state-censorship-control-and-increasing-isolation ）。
  - 2026-06 厂商自述超过 90% 服务器被封（https://www.techradar.com/vpn/vpn-privacy-security/amnezia-vpn-improves-anti-censorship-protocol-as-russia-escalates-anti-vpn-regime ）。
  - 2026-08 有运营方称是 IP 级封锁，改协议无用（https://hub.xeovo.com/posts/208-it-looks-like-amneziawg-locations-started-to-get-blocked-in-russia ）。
  - 在中国的战绩和性能损耗：没找到可信数据。
- **udp2raw / Phantun：** udp2raw 自述伪 TCP 可被 ISP 区分；Phantun 每包开销 12 字节、无加密。https://github.com/gfwsucks/udp2raw-tunnel 、https://github.com/dndx/phantun
- **TLS 套 TLS：** 23 种代理配置的识别率都在 70% 以上。https://www.usenix.org/conference/usenixsecurity24/presentation/xue
- **WARP 转 MASQUE：** 理由是走 443 混入 HTTP/3、可换拥塞控制和 FIPS 套件（https://blog.cloudflare.com/zero-trust-warp-with-a-masque ）；在中国仍因 IP 或 SNI 不通（上面的 #418）。
- **哪种存活最好：** 没有可信的 2026 横向测量。判断：封锁重心已转向 IP 和行为，用自有域名、伪装成 TLS/QUIC 的 443 流量最稳，你们现有协议属于这一类。

## 3. 架构
「访客 → 机房伪装入口 → 盒子主动拨出 WireGuard 到入口」是合理设计（判断）：
- 该路径不需要打洞，盒子只做内核 WireGuard 加 NAT。
- 入口应终结 TCP 再经 WireGuard 发往盒子，不要把 WireGuard 包整体塞进可靠流。这样没有 UDP-in-TCP 崩塌和 MTU 叠加，双重加密的 CPU 成本也落在机房。
- 代价是三角绕路，入口要选在离盒子近的区域。
- 用户的 QUIC/UDP 流量无法拆分，只能按数据报转发，或屏蔽 UDP 443 迫使回落 TCP（判断）。

## 4. 拥塞控制
- **TCP 拆分仍然有效，但 BBRv3 在有损路径上明显退步。** 2% 丢包下端到端 CUBIC 利用率约 0，BBRv1 约 0.8，BBRv3 约 0.15；拆分后 v3 才回到 0.5–0.85。QUIC 无法被透明拆分。https://www.usenix.org/system/files/atc25-yuan.pdf
- **BBRv3 未进主线：** Google 最新树外分支是 `bbr-v3-2026-09-16-01`（https://github.com/google/bbr/branches/all ）；二手来源称 v7.3-rc7 仍是 v1（https://github.com/cordanaLLM/nucleus/issues/192 ）。
- **FEC：** UDPspeeder 的 20:10 示例约多耗 50% 带宽。https://github.com/wangyu-/UDPspeeder
- **Brutal：** 固定速率发送，官方警告设得过高会造成拥塞。https://v2.hysteria.network/docs/advanced/Full-Client-Config/ 判断：不适合放在家宽上行。
- **最简做法（判断）：** 盒子不加任何拥塞控制；有损的跨境段交给入口上现有的自定义拥塞控制；入口到盒子段用内核 BBRv1。

## 5. 内核
- **版本：** 主线 7.4-rc2，稳定版 7.3.3（11-06），7.2 已 EOL；LTS 是 6.18、6.12（均到 2028-12）、6.6，尚无 7.x LTS。https://www.kernel.org/ 、https://www.kernel.org/category/releases.html
- **Debian 13：** 稳定版 6.12.94，安全更新 6.12.116，trixie-backports 是 7.2.12（上游已 EOL）。https://tracker.debian.org/pkg/linux
- **WireGuard 改进：** 默认 threaded NAPI（https://git.zx2c4.com/wireguard-linux/commit/?id=db9ae3b6b43c79b1ba87eea849fd65efa05b4b2e ）；6.13 加 BIG TCP GSO，约 +15%（https://www.phoronix.com/news/Linux-6.13-Networking ）。
- **Wyse 3040 的 eMMC：** Debian 6.12 能识别；forky 默认识别不到，手动加载 `sdhci`、`sdhci_acpi`、`mmc_block` 后正常，线程里怀疑是 initramfs 没带模块，但根因未确认。https://dietpi.com/forum/t/internal-emmc-not-visible-on-dell-wyse-3040/25433
- **没查到的：** flowtable、CAKE、io_uring 零拷贝、加密库在 7.x 的具体变化，以及 Cherry Trail 在 6.12 以上的 C-state 报告。
- **判断：** 用 6.12 或 6.18 LTS；追 7.x 意味着跟着非 LTS 滚动，且 AmneziaWG 这类树外模块更难维护。

## 6. NAT 穿透
- **多个 STUN 服务器不提高打洞成功率。** 作用是判断 NAT 映射类型和保证可用性。https://tailscale.com/blog/how-nat-traversal-works
- **大陆可达性：** Google STUN 2019 年被报告封锁，数据较旧（https://bugs.torproject.org/30579 ）；小米、B 站、腾讯的 STUN 未证实。判断：在自家节点上自建。
- **公开的直连率：** Tailscale 称远高于 90%（https://tailscale.com/blog/nat-traversal-improvements-pt-1 ）；libp2p 实测 440 万次约 70%，TCP 与 QUIC 无显著差别（https://arxiv.org/abs/2510.27500 ）。
- **生日攻击（一侧困难 NAT）：** 256 个端口加 1024 次探测约 98%；两侧都困难基本只能中继（同上 Tailscale 文章）。
- **端口映射：** 2019 年一次扫描中 76% 的路由器开着 UPnP，样本有偏。https://www.trendmicro.com/en_us/research/19/c/upnp-enabled-connected-devices-in-home-unpatched-known-vulnerabilities.html
- **IPv6：** Google 口径全球约 50%（https://pulse.internetsociety.org/en/blog/18-years-later-ipv6-reaches-majority/ ）；各国数字来源互相矛盾，家用路由器入站默认策略没查到。
- **TURN：** Cloudflare $0.05/GB、免费 1000 GB，超过 50–100 Mbps 可能丢包，中国流量接到境外节点（https://developers.cloudflare.com/realtime/turn/faq/ ）；Twilio $0.40–0.80/GB（https://www.twilio.com/en-us/stun-turn/pricing ）。条款是否允许中继 VPN 流量、Google 是否提供 TURN：未证实。判断：用自己的 40 个节点做中继。

## 7. 吞吐
- **没有 Z8350 的实测。** 最近的参照是 J1900 作为 WireGuard 端点跑到 874 Mbps（2017 年，内核 3.16，非转发）。https://lists.zx2c4.com/pipermail/wireguard/2017-December/002204.html
- **flowtable：** 软件快速路径在服务器上约 +58%。https://firewalld.org/2023/05/nftables-flowtable
- **硬件分流：** 消费级路由器靠 SoC 硬件流表；flowtable 的硬件 offload 需要网卡驱动支持（https://docs.kernel.org/next/networking/nf_flowtable.html ）。判断：Realtek 千兆网卡没有，x86 瘦客户机没有等价物。
- **判断：** 纯 NAT 可接近千兆，内核 WireGuard 约 300–600 Mbps；实际上限多半是家宽上行。

## 结论
- **大陆路径传输：** App 用现有伪装 QUIC/TLS 协议连机房入口（TCP 在此终结，自定义拥塞控制）；入口经标准 WireGuard 连到主动拨出的盒子。大陆用户 v1 不走直连打洞，盒子实现保持唯一。
- **直连率三大杠杆：** 盒子侧 UPnP/NAT-PMP/PCP 映射并自动续期；两端 IPv6；访客在困难 NAT 时用生日探测。
- **盒子吞吐上限：** 按 300–600 Mbps 的 WireGuard 规划，需实测。
- **仍不确定：**
  - 2026 年 GFW 对各协议的实际封锁强度。
  - Z8350 的真实数字。
  - 7.x 内核在 3040 上的 eMMC 和稳定性。
  - 国内 STUN 的可达性。
  - Cloudflare TURN 的条款。
