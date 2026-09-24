# VPN 节点云商比价（按「送达用户的流量」折合）

> 2026-09-24 调研。起因：Lightsail 悉尼 2 台 9 月被配额掐断（au-1 9/18 起停、au-2 预计 9/28），需要流量更高、像 Lightsail 一样好操作的替代/补充。
> **✔ = 已用厂商公开 API 亲自核实**；其余来自官方页面（见文末来源），「待核实」= 未能在官方页面确认。价格随时会变，下单前再核一次。

## 1. 折合口径（为什么不能直接比名义配额）

VPN 节点上，用户下载的每一个字节都会先从目标站**进**节点，再从节点**出**给用户，所以 **入站 ≈ 出站**。我们 2026-09 的实测：

| 样本 | 入站 | 出站 | (入 + 出) ÷ 出 |
|---|---|---|---|
| au-1（Lightsail 悉尼） | 475.0 GiB | 524.7 GiB | 1.91 |
| au-2（Lightsail 悉尼） | 352.3 GiB | 384.7 GiB | 1.92 |
| 美西池（Cost Explorer USW2） | 2661 GB | 2905 GB | 1.92 |

因此 **用户流量 ≈ 节点出站流量**，不同计费口径的折合系数：

| 计费口径 | 1 TB 用户流量消耗的配额 | 折合系数 | 名义配额的实际可用比例 |
|---|---|---|---|
| 入 + 出合计（Lightsail、HostHatch、Kamatera） | 1.92 TB | ×1.92 | 52% |
| 只计出站（Vultr、Linode、DO、BinaryLane 等） | 1 TB | ×1 | 100% |
| 取入 / 出较大值（DMIT Tier 1 等） | 1 TB | ×1 | 100% |

下表的「折合 $/TB」一律是 **每 TB 送达用户流量的成本** = 月费 ÷ 名义配额 × 折合系数。

## 2. 对比表

| 厂商 / 套餐 | 月费 | 名义配额 | 计费口径 | 系数 | **单台可承载用户流量** | **折合 $/TB** | 超额（折合） | 悉尼 | 备注 |
|---|---|---|---|---|---|---|---|---|---|
| **Lightsail** 多数区（现用） | $7 | 2 TB | 入 + 出 | ×1.92 | **1.04 TB** | **$6.72** | ≈ $92/TB（只按出站部分计费） | — | 现状；按「同区 + 同 bundle」池化 |
| **Lightsail** 悉尼 / 香港（现用） | $7 | 1 TB | 入 + 出 | ×1.92 | **0.52 TB** | **$13.44** | 悉尼 ≈ $117/TB | ✅ | 现用最贵；悉尼无更划算的 bundle |
| **Vultr** `vhp-1c-1gb` ✔ | $6 | 2 TB | 只计出站 | ×1 | **2 TB** | **$2.93** | $10/TB | ✅ | 全账号全球池化 + 每账号送 2 TB；无 HK |
| Vultr `vc2-1c-1gb` ✔ | $5 | 1 TB | 只计出站 | ×1 | 1 TB | $4.88 | $10/TB | ✅ | |
| **Linode (Akamai)** Nanode ✔ | $5 | 1 TB | 只计出站 | ×1 | **1 TB** | **$5.00** | **$5/TB（与套餐同价）** | ✅ | 全球池化（雅加达 / 圣保罗除外）；无 KR / HK |
| DigitalOcean | $6 | 1 TB | 只计出站 | ×1 | 1 TB | $6.00 | $10/TB | ✅ | 团队级池化；无 JP / KR / HK |
| **BinaryLane**（澳洲本土） | ≈ $3.2（AUD 4.90） | 1 TB | 只计出站 | ×1 | 1 TB | **≈ $3.2** | ≈ $6.5/TB，不限速 | ✅ | 汇率待核实；澳洲各城 + SG；账户内池化 |
| Hetzner US CPX11 | $20.49 | 1 TB | 只计出站 | ×1 | 1 TB + 超额 | 10 TB 时 ≈ $3.1 | **$1.2/TB** | ❌ | 只有美国（俄勒冈 / 弗吉尼亚）；SIN 超额 $8.49/TB |
| OVH VPS 美 / 加 | ≈ $4.5 起 | 不限（500 Mbps） | — | — | 受带宽限制 | ≈ 0 | — | ⚠️ APAC 限 0.5–3 TB 后降到 10 Mbps | 仅北美不限量；起步价可能需长约（待核实） |
| Oracle OCI | 算力另计（A1 近乎免费） | 每月前 10 TB 免费 | 只计出站 | ×1 | 10 TB | $0 → 之后 APAC $25 | APAC $25/TB、北美 $8.5/TB | ✅ | 注册 / 审核严；免费账户有被回收报告（待核实） |
| LightNode 东京 / 香港（现用） | $7.71 / $10.41 | 1 TB | 只计出站 | ×1 | 1 TB | $7.71 / $10.41 | 按量 | ❌ | 已在用；2 次免费换 IP、小时计费 |
| DMIT Tier 1（HK / 东京 / LA） | $49.90 | 32 TB | 取较大值 | ×1 | 32 TB | **$1.56** | 待核实 | ❌ | 无中国线路优化，需先测 |
| DMIT Premium LAX（CN2 GIA） | $10.90 | 1 TB | 待核实 | — | — | ≥ $10.9 | 待核实 | ❌ | 精品线路；HK / 东京 Premium $44–80/TB |
| 搬瓦工（现用，如 us-la.bwh.wm07） | — | 2 TB | **待核实** | ×1 或 ×1.92 | 1.04–2 TB | 视口径 | 到 100% 断整机网络 | ❌ | wm07 保守按入 + 出配置 |
| HostHatch 悉尼 | $4 | 0.5 TB | 入 + 出 | ×1.92 | 0.26 TB | $15.4 | 限速 5 Mbps | ✅ | 无 API（"upcoming"）；排除 |
| Kamatera | ≈ $4 | 1 TB（亚洲） | 入 + 出 | ×1.92 | 0.52 TB | ≈ $7.7 | $10/TB | ✅ | 数据待核实 |
| ~~腾讯轻量（国际）~~ | $6.50 | 2 TB | 只计出站 | ×1 | 2 TB | $3.17 | $77–130/TB | ❌ | **文档明文禁代理 / 隧道**、公网 IP 不可换、20–30 Mbps 峰值 |
| ~~阿里 SWAS（国际）~~ | $19 | 4 TB | 只计出站 | ×1 | 4 TB | $4.75 | $76–153/TB | ❌ | 条款禁协助规避任何法域政策、IP 不可换、4–30 Mbps |
| ~~华为 Flexus L~~ | $9 | 3 TB | 只计出站 | ×1 | 3 TB | $3.00 | $114/TB | ❌ | 30 Mbps 峰值，单台承载用户数受限 |

## 3. 结论

1. **同样 1 TB 用户流量的价格**：悉尼 Lightsail $13.44 → Vultr $2.93 / BinaryLane ≈ $3.2 / Linode $5（**便宜 3–4.5 倍**）；日 / 韩 / 新 / 美 Lightsail $6.72 → Vultr $2.93（**约 2.3 倍**）。
2. **超额是本质差距**：Lightsail 超额折合 $92–117/TB，所以只能硬掐断（au-1 当前状态）。Linode 超额与套餐同价，可以不掐断；Vultr $10/TB 也可接受。
3. **全球池化**：Vultr / Linode 全账号全球共享流量，不再出现「单台先掐死、同池邻居的余量用不上」——这正是当前 Lightsail 按区域池化、sidecar 却按单台掐断的结构性错配（见 memory `reference_lightsail_transfer_pool_same_region_same_bundle`）。
4. **还没测的，也是决定性的**：**大陆可达性 / IP 段封锁情况**。两路调研都没有找到可靠来源，必须自己测；便宜但连不上等于零。

## 4. 建议的试点路径

1. **先试点，不写代码**：Vultr 悉尼 `vhp-1c-1gb`（$6，2 TB 用户流量）+ Linode 悉尼 Nanode（$5）各一台，走 `ssh_standalone` 零代码接入（节点自计量、自掐断）。顺带解决 9/28 前后的 AU 断服，也比再加一台 Lightsail 便宜。
2. **大陆探测**：经 tailnet 的 cn-gw（贵州电信出口）测 TCP / QUIC 可达，以 au-2 为对照（方法见 memory `reference_mainland_probe_via_tailscale_cn_gw`）。
3. **一周真实用户**：对比 auth 次数、重连、丢包与 au-2。
4. **过关后**再为 Center 写 Vultr / Linode provider：实现 `api/cloudprovider.Provider` 的 9 个方法，按历史经验 350–650 行（参考 `aws_lightsail.go` / `bandwagon.go`）。

**前置（需要人工）**：注册 Vultr / Linode 账号并提供 API token；开机前通读两家 AUP（Linode 2026-08-01 新版 AUP 仅有 PDF，尚未读）。

### 新节点 `.env` 计量设置

| 厂商口径 | `K2_NODE_TRAFFIC_BILLING_MODE` | `K2_NODE_TRAFFIC_LIMIT_GB` |
|---|---|---|
| 入 + 出合计（Lightsail） | `sum`（**必须**，留空会少算一半） | 套餐配额 − 余量；首月按比例折算 |
| 只计出站 / 取较大值（Vultr、Linode、BinaryLane…） | 留空（`max` ≈ 出站） | 套餐配额；全球池化的厂商可按池分配或放宽 |

`K2_NODE_BILLING_START_DATE` 的日期必须是厂商真实的重置日（Lightsail = 每月 1 日，搬瓦工 = 面板上的 "Next reset"），详见 `.claude/skills/kaitu-node-ops/references/metering.md` Part B。

## 5. 我们自己的需求基线（2026-09，入 + 出，按近 24 h 速率外推到月底）

| 区域 | 本月预计用量 | 当前实例 |
|---|---|---|
| 美西（us-west-2） | ≈ 8.5 TB | 5 × `micro_3_0` |
| 东京（ap-northeast-1） | ≈ 7.9 TB | 5 × `micro_3_0` |
| 加拿大 / 首尔 / 新加坡 | 各 ≈ 4.6 TB | 各 3 × `micro_3_0` |
| 悉尼（ap-southeast-2） | ≈ 2.1 TB（被掐断压着，真实需求更高） | 2 × `micro_3_2` |
| 香港（ap-east-1） | ≈ 1.8 TB | 2 × `micro_3_1` |

合计约 34 TB（入 + 出）≈ 17.7 TB 用户流量；Lightsail 月费约 23 × $7 ≈ $161。9 月 Cost Explorer 各区均无 Overage 行（8 月悉尼有过 $39.53）。

## 来源

- Vultr：https://api.vultr.com/v2/plans （公开 API，2026-09-24 实测）· https://blogs.vultr.com/Vultr-Announces-Reduced-Bandwidth-Pricing-2-Tb-Of-Free-Monthly-Egress-Free-Ingress-And-Global-Pooling · https://docs.vultr.com/support/platform/billing/what-is-the-bandwidth-overage-rate
- Linode：https://api.linode.com/v4/linode/types · https://api.linode.com/v4/network-transfer/prices （公开 API，2026-09-24 实测）· https://techdocs.akamai.com/cloud-computing/docs/network-transfer-usage-and-costs
- DigitalOcean：https://docs.digitalocean.com/platform/billing/bandwidth/ · https://www.digitalocean.com/legal/acceptable-use-policy
- BinaryLane：https://www.binarylane.com.au/vps-hosting/linux-vps · https://support.binarylane.com.au/support/solutions/articles/11000037696-is-network-data-in-both-directions-counted- · https://support.binarylane.com.au/support/solutions/articles/11000060080-is-network-speed-throttled-if-my-server-exceeds-its-quota-
- Hetzner：https://docs.hetzner.com/cloud/billing/faq/
- OVHcloud：https://www.ovhcloud.com/asia/vps/vps-australia/ · https://www.ovhcloud.com/asia/public-cloud/prices/
- Oracle：https://www.oracle.com/cloud/networking/pricing/
- LightNode：https://doc.lightnode.com/FAQ/FAQ.html · https://doc.lightnode.com/Network/IPchange.html
- DMIT：https://www.dmit.io/pages/pricing
- 搬瓦工：https://bandwagonhost.com/cart.php · https://bandwagonhost.com/tos-frame.php
- HostHatch：https://hosthatch.com/products · https://hosthatch.com/terms-of-service
- Kamatera：https://www.kamatera.com/faq/answer/will-i-ever-be-charged-extra-for-internet-traffic-on-a-monthly-server-plan/
- 腾讯轻量：https://www.tencentcloud.com/document/product/1103/47794 · https://www.tencentcloud.com/document/product/1103/41257
- 阿里 SWAS：https://www.alibabacloud.com/en/product/swas/pricing · https://www.alibabacloud.com/help/en/simple-application-server/product-overview/limits · https://www.alibabacloud.com/help/en/legal/latest/alibaba-cloud-international-website-product-terms-of-service-v-3-8-0
- 华为 Flexus L：https://www.huaweicloud.com/intl/en-us/product/flexus-l.html · https://support.huaweicloud.com/intl/en-us/productdesc-flexusl/pd_01_0003.html
- Lightsail：`aws lightsail get-bundles`（2026-09-23 实测）；池化与双向计费规则见 AWS Lightsail 定价页
