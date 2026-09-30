k2r「断线即断网」kill switch：隧道不可用时 LAN 是直连还是断网，目前是直连——待讨论后决定

状态：待讨论（2026-09-30 提出，未定方案，未排期）。

现状（k2 f4ba63e，testlab OpenWrt 25.12 实测）：
- k2r 的语义是「没连上 = 直连」。开机未连上、`k2r down`、`k2r up` 失败、订阅节点全部耗尽后的自动重连期间，拦截规则都不在，LAN 流量（含 DNS）直接走运营商。
- 订阅耗尽后的重连按退避表重试（3 s → 15 s → 30 s → 60 s → 120 s → 300 s），节点恢复后最长还要再直连一个步长（≤ 5 分钟）。
- 纯 k2v5:// 配置不走这条路：引擎不死、规则保留，断线期间已经是 fail-closed。
- 残留规则在两个后端上的含义不同：iptables 的 TPROXY 没有监听者时丢包（fail-closed）；nftables 的 tproxy 没有监听者时是 no-op，TCP/UDP 照常直连，只有被重定向到死端口的 DNS 失败。所以「留着规则」在 OpenWrt（nft）上并不等于断网。
- 残留规则还会把路由器自己的 DNS 重定向到死端口（`dnsredir_out`），重连时解析订阅 API / 节点域名会受影响——kill switch 不能简单做成「断线时不拆规则」。

需要讨论的问题：
1. 产品上要不要 kill switch？默认开还是关？（隐私诉求 vs 节点故障时全家断网）
2. 范围：只拦「本该走隧道的流量」，还是全部 LAN 出站？直连规则（国内直连）在断线时是否照常放行？
3. 触发面：断线重连期间、开机未连上、`k2r up` 失败、`k2r down`（用户主动断开应当恢复直连）各自怎么处理。
4. 实现：nft 上需要显式 drop/reject 规则（不能依赖 tproxy 无监听者），并且要给守护进程自己的出站（订阅拉取、节点握手、DNS）留口子；iptables / nft shell / nft netlink 三个后端要一致。
5. 逃生口：kill switch 生效时用户怎么自救（面板 / 配套 App 是否还能从 LAN 访问，`k2r down` 必须立即解除）。
6. 守护进程崩溃 / 被杀 / OTA 重启的交接窗口与 kill switch 的关系（目前靠残留规则交接，nft 上数据面是直连的）。

验证要求：必须在真路由器上测（`k2/test/k2r-lifecycle/router-check.sh` 加对应步骤），Docker UAT 覆盖不到 procd、nft 无监听者语义和 busybox 环境。

背景：`k2/gateway/CLAUDE.md`「A tunnel whose engine gave up is brought back by the gateway」一条。
