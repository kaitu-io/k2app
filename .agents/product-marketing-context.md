# Product Marketing Context — Kaitu / Overleap

*Last updated: 2026-07-15 —— 品牌架构为 2026-07-14 的「完全隔离」决策，非 04-21 的母品牌层级*

> 本文档被所有 `marketing-skills:*` 和 `kaitu-growth` skill 引用，作为品牌、定位、用户、竞品、文案声调的单一事实源。更新时用 `/product-marketing-context` skill。

---

## Product Overview

**One-liner (zh):** 别人断线，你满速 —— 高丢包弱网下依然稳定的隐身 VPN。
**One-liner (en):** When others drop, you stay on line-rate. A stealth VPN designed for high-loss, high-censorship networks.

**What it does:** 订阅制 VPN，主要面向中国大陆及其他高审查网络用户。自研 k2 隧道协议 = ECH 加密 TLS 握手 + QUIC/TCP-WebSocket 双栈 + k2cc 审查感知拥塞控制。在传统 VPN 协议归零的环境下仍能保持稳定吞吐。

**Product category:** 翻墙工具 / 科学上网 / VPN 推荐 / stealth VPN / censorship-circumvention tunnel。竞争货架：WireGuard、Shadowsocks、VLESS+Reality、Hysteria2、Clash、V2Ray、Astrill、ExpressVPN、Mullvad。

**Product type:** SaaS 订阅（direct-to-consumer） + 自托管服务端二进制（`k2s`） + 预售硬件（`k2r` OpenWrt 路由器）。

**Business model:** 预付费多年订阅，USD 计价。当前在售：`basic` 档 × 1/2/3/5 年四档时长（越长越便宜）。`lite / family / business` 档已设计（Spec `2026-04-20-tier-rename-design.md`）但未上线。

**Pricing rails:** 支付宝 · 微信 · 银联 · 信用卡 · USDT/USDC/ETH/BTC。**退款：** 首次购买 7 天内 <1GB 可退款到钱包，钱包可续费或提现到加密货币。

---

## Brand Architecture

**两个完全隔离的对等品牌**（2026-07-14 决策，推翻了 2026-04-21 的母品牌 / 子产品层级）：

| 品牌 | 市场 | 主渠道 |
|---|---|---|
| **开途**（kaitu.io） | 中国 | kaitu.io · 小红书 / 知乎 / 微博 · 中国区 App Store · 国内分销 |
| **Overleap**（overleap.io） | 海外 | overleap.io · 全球 App Store / Play · 英文社交 / 技术社区 |
| **协议层（不属于任一品牌）** | 全球共享 | k2（隧道）· k2cc（拥塞控制）· k2s（自托管服务端）· k2r（路由器硬件）—— 技术文档 · GitHub · benchmark |

**没有母子关系**：两者不是"公司层 / 产品层"，是两个各自独立运作的产品品牌，各有自己的用户池、定价面、支付渠道与叙事。

**底层产品相同**（同一协议栈、同一客户端代码库、同一服务端）—— 这是**内部事实，不是对外叙事**。任何面向用户的语境都不说"这两个是一个东西"。

### 品牌隔离规则（硬规则）

- 中国面**只**出现「开途」。中文用户面向语境**禁用 "Kaitu" 裸词** —— "kaitu" 只作为域名 / bundle id（kaitu.io、`io.kaitu`）出现，不进正文。
- 海外面**只**出现 "Overleap"，**禁用 "Kaitu" 裸词** —— 英文用户无背景，且 Google 会把 "Kaitu" 纠错成 "kaitai"（Kaitai Struct 二进制解析器）。
- **两边互不提及。** 没有衔接句、没有 footer 关联、没有 About 页交叉引用。~~"Kaitu by Overleap"~~ / ~~"Kaitu, a product of Overleap"~~ **已作废，禁止使用**。
- **唯一例外：法务文书署名。** ToS / 隐私政策 / footer 的法律实体署名两边都是 **Overleap LLC**。这是刻意收窄的单点例外，不要扩大到叙事层。

详细用词规则、禁用组合、SEO 关键词矩阵见 `docs/marketing/brand-naming-strategy.md`。

### 品牌开放项

不阻塞本文档使用，但需要单独决策与执行：

- **Overleap 法律实体的司法辖区未定** —— 美国 / 新加坡 / 香港 / 开曼均在考虑，Inc. / LLC / Ltd. 形态待定。法务文书已按 **Overleap LLC** 署名，但实体注册尚未落地；过渡期商户收款由现有运营主体承接，不硬绑 "Overleap Inc."。
- **Overleap 侧数据为空** —— 付费主力画像 / 主要渠道 / 支付方式都还是 TBD，等首批付费用户数据回来再精修（见下面 Market Positioning Matrix）。

### Market Positioning Matrix

| 维度 | Kaitu（中国） | Overleap（海外） |
|---|---|---|
| 核心叙事 | 抗 GFW 稳定 + 技术可信 | 隐私 + 开源 + 协议透明 |
| 付费主力 | 实用派 | TBD（等 100+ 付费用户数据） |
| 主要渠道 | SEO / KOL / 小红书 / 分销 / Twitter | TBD |
| 本地化 | zh-CN 母版 | en-US 母版 |
| 支付方式 | 支付宝 / 微信 / 银联 / 信用卡 / 加密货币 | 信用卡 / 加密货币（TBD） |
| 客服 | WhatsApp + 邮件 + 工单 + AI | 邮件 + 工单（TBD） |
| iOS 分发 | 中国区 App Store | 美区 / 欧区 App Store |

**本文档中国部分成熟，海外部分大部分为 TBD —— 等 Overleap 积累数据后再精修一次。**

---

## Target Audience

### 中国市场（Kaitu）ICP 优先级分层

| 段 | 优先级 | 所占心智 | 用途 |
|---|---|---|---|
| **实用派** | ⭐ 付费主力 | 所有商业决策围绕这群人 | 营收核心 |
| **技术派** | SEO / 信任引擎 | 不一定付费，GitHub / V2EX / 掘金 / Twitter 口碑来源 | `/k2/` 协议文档、`k2s` 自托管、benchmark 内容都给他们看 |
| **家长型** | 未来机会段 | family tier 上线（Spec B）后的增量 | 当前只做不流失，等档位上线再主推 |
| **自托管派** | 社区 / 反黑箱信任 | 不付费但 `k2s` 的存在是"我们不是黑箱"的信号 | 开发者关系、GitHub stars、协议公信力 |

### 海外市场（Overleap）ICP 假设（待验证）

当前 ICP 未经数据验证。三个候选 wedge：

- **候选 1：中国出海华人**（留学生、外派、移民）—— 熟悉 Kaitu 心智，切换成本低；但市场天花板有限。
- **候选 2：隐私敏感的西方技术用户**（Mullvad / IVPN / ProtonVPN 的受众）—— 对 ECH / 自托管 / no-log 有共鸣；需要全新内容叙事。
- **候选 3：其他审查市场**（伊朗、俄罗斯、土耳其）—— k2 协议技术上有效，但获客、本地化、支付都需要独立投入。

**Action needed**：等 Overleap 首批付费用户上线后做用户访谈，决定 launch wedge。在此之前，海外营销保守投入。

### Jobs to be Done

1. 稳定访问某个具体被墙服务（Google、YouTube、ChatGPT、X、GitHub、学术资源）—— 日常使用。
2. GFW 升级期间不挂 —— "别的都挂的时候你还能用"。
3. 一个账号覆盖多设备 / 多家人。
4. 从挂掉 / 变慢 / 过于复杂的上一个工具切换过来。

### 典型使用场景

- 工作：海外供应商沟通、Google Workspace / Slack / GitHub。
- 学习：海外留学生 / 跨境研究人员 / 学术资源访问。
- 家庭日常：YouTube / Netflix / 国际新闻（尤其在敏感日期前后）。
- 出境回国：国人在海外需要国内服务（用 "全局" 模式反向路由）。

---

## Personas

| Persona | Cares about | Main challenge | Value we promise |
|---|---|---|---|
| **实用派** (默认消费者 / 中国付费主力) | "能用就行，不折腾" | Other tools keep breaking; 配置太复杂 | 一键安装、一键连接、家人都能用 |
| **技术派** (Clash / V2Ray 老用户) | Protocol design, benchmarks, transparency | 工具每次 GFW 升级就挂，维护累 | k2cc 26% 丢包满速、ECH 隐身、端口复用、CT 零暴露 |
| **家长型** (Family decision-maker) | Simplicity, parents-can-use, kids-covered | 一个家要买多个订阅，老工具太技术 | 一账号 5 设备、其他设备 USB 辅助安装、WhatsApp 客服、family tier 路由器方案 |
| **自托管派** (Self-host developer) | Control, privacy, not trusting provider | 不想日志给公司，想自己掌控 | `k2s` 二进制、一行 curl 部署、CT 日志零暴露 |

---

## Problems & Pain Points

**核心问题：** 在中国大陆，每个翻墙工具最终都会失效 —— 被 GFW 升级封掉、被 ISP QoS 限速、或配置复杂到不想再维护。用户在工具间来回跳（Clash → V2Ray → Shadowsocks → ...），每次切换损失几天生产力。

**现有方案的失败模式：**

| 方案 | 失败原因 |
|---|---|
| WireGuard | UDP 明文无 TLS 伪装，GFW 下基本不能用 |
| Shadowsocks | 轻量加密无伪装，被 ML-DPI 识别 |
| VLESS + Reality | 有 TLS 指纹伪装，但无 ECH、无 QUIC、无 TCP 降级 |
| Hysteria2 | 有 QUIC 有拥塞控制，但无 ECH、无 TCP 降级，UDP 一封就挂 |
| Clash + 机场订阅 | 能用时很强，但订阅 URL 维护 + 节点轮换 + 配置调优 → 维护负担 |
| 海外商业 VPN（Express/Nord/Surfshark） | 不为 GFW 设计，通用隧道秒被 DPI 识别 |

**代价：**
- 工具挂的时候浪费几小时到几天 debug（尤其在敏感日期前后）。
- 多份订阅 "一份备份"，费用叠加。
- 工作 / 学习在关键时刻被切断。
- 给家人推荐失败 → 家人持续找自己做技术支持。

**情绪张力：** 焦虑（今天能用明天会不会挂）、尴尬（推荐给家人失败）、偏执（付费被追踪）、担忧（背"风险"订阅）。

---

## Competitive Landscape

### 中国市场（Kaitu 视角）

| 层级 | 具体竞品 | 它们的定位 | 我们的反制叙事 |
|---|---|---|---|
| **直接 L1**（高端 / 技术派心智） | Astrill | 老牌 OpenWeb/StealthVPN 协议，~$70/年起，UI 陈旧 | 协议代际更新（k2 vs StealthVPN）+ 价格更优 + Apple 公证代码签名 |
| **直接 L2**（消费流量大户） | LetsVPN、闪电 VPN、快连 VPN、云墙等 | 面向非技术用户的简化 UI，底层多为 SS/V2Ray 套壳 | "不是 SS 套壳"是核心叙事 —— 用 k2 vs SS/V2Ray 的公开技术对比 |
| **直接 L3**（国际品牌） | ExpressVPN、NordVPN、Surfshark | 海外主流消费 VPN，不专门为 GFW 优化 | "为 GFW 设计的协议" vs "全球通用的 OpenVPN/WireGuard" |
| **二级**（DIY 机场） | Clash (Meta/Verge/for Windows) + 机场订阅 | 技术派默认；机场底下多为 SS/V2Ray/Hysteria | 不当面对抗 Clash 生态 —— 利用 "Clash 心智保留、协议用 k2、服务器我们管" |
| **二级**（自建） | 自建 V2Ray / Hysteria2 / XTLS on VPS | 最大控制 + 最大维护负担 | 两头吃：非极客卖订阅，极客卖 `k2s` 二进制 |
| **间接**（替代行为） | 凑合 / ChatGPT 镜像站 / 企业跨境专线 | 放弃或零散应对 | 强调 "每天稳定使用" 的 ROI：时间成本 vs 订阅成本 |

### 海外市场（Overleap 视角 — 早期待完善）

| 层级 | 具体竞品 | 定位 |
|---|---|---|
| **直接（隐私向）** | Mullvad、IVPN、ProtonVPN | 强隐私、匿名支付、no-log |
| **直接（品牌流量）** | ExpressVPN、NordVPN、Surfshark | 主流消费者 VPN |
| **二级（技术替代）** | Tailscale、自建 WireGuard | 不是消费 VPN，但隐私/技术用户心智占用类似 |
| **细分（审查市场）** | Lantern、Psiphon、Tor、Outline | 人道主义 / 开源 / 非商业 |

### 竞争话术原则

- 对外比较页 & 广告**允许点名友商**（现在 marketing 大家都这么做）。
- **保持事实基调 + 中立比较表 + 不情绪化贬低**。
- 禁用词："垃圾"、"骗子"、"跑路"、"智商税"。
- 攻击协议 / 产品，不攻击公司 / 人。
- 尽量使用协议层对比（k2 vs SS/V2Ray/Reality/Hy2）而不是品牌对比 —— 因为底层一致，一次解释覆盖所有品牌。

---

## Differentiation

**核心差异化（按独特性排序）：**

1. **k2cc 审查感知拥塞控制** —— 唯一主流翻墙协议能区分 "GFW 的 QoS 丢包" 与 "正常网络拥塞"。自测数据：26% 丢包下比 BBR/Brutal 吞吐高 2–5×。
2. **ECH 加密 SNI** —— 唯一主流翻墙协议实现 ECH，TLS 握手完全 DPI 不可检测。
3. **QUIC + TCP-WebSocket 单端口双栈** —— 单个 443 端口同时处理 QUIC、TCP-WS、真实 HTTPS。一边被封，另一边继续。
4. **CT 日志零暴露** —— 自签名 + 证书 Pin，服务端域名永不出现在公开 CT 日志，不可被扫描器发现。
5. **一行命令自托管** —— `curl | sudo sh` 装好 `k2s`，自动生成 `k2://` URI，无需配证书。
6. **一账号 5 设备 + 家庭共用** —— 不按设备 / 不按账号分。
7. **中国区 App Store 可直接下载 iOS 客户端** —— 大多数竞品做不到（被下架或需要海外 Apple ID）。

**为什么这更好：** 用户不用每次 GFW 升级都换工具；家人不需要碰配置文件；技术用户拿到的是基准数字而不是营销形容词。

---

## Objections & Responses

| 异议 | 反驳 |
|---|---|
| "我已经用 Clash + 某机场，够了" | "机场底下还是 Shadowsocks/V2Ray 协议，下次 GFW 大动作依然会挂。k2 协议在抗封锁维度是另一个量级。免费试用，用你自己的网络对比一下。" |
| "没听说过，不敢信" | "GitHub kaitu-io 开源、协议文档公开、`k2s` 可自部署 —— 你可以完全不信任我们，自己跑服务端，只用客户端连接。" |
| "付费会被追踪吗？" | "商户描述中性，银行看不到具体购买内容；也支持 USDT/USDC/BTC/ETH 匿名付款。" |
| "付完用不了怎么办？" | "注册送试用额度，不用信用卡预授权。首付 7 天内用量 <1GB 无理由退款到钱包，可提现到加密货币。" |
| "5 台设备够吗？" | "家人 + 手机 + 电脑 + 平板通常 3–4 台。超过 5 台在第 6 台登录时自动踢最早那台，不是硬锁。多设备场景可以等即将上线的 family/business 档。" |

### Anti-persona（我们不想要的用户）

- 24×7 跑 P2P / 高量下载的 —— 7 天 <1GB 退款门槛专门过滤。
- 要求永久免费 —— 提供试用但无永久免费档，free-rider LTV 为负。
- 想自定义服务器列表 / 切换协议栈 —— 那应该用 `k2s` + BYO-VPS。
- 与中国访问场景无关的用户（如"从英国看 Netflix US"）—— 不是我们优化的场景。

---

## Switching Dynamics (JTBD Four Forces)

**Push**（离开现有工具）：
- 上次 GFW 升级被封了
- 晚上 / 高峰期速度掉到不可用
- Clash 配置维护累、订阅 URL 老失效
- 竞品被中国区 App Store 下架 / 海外 Apple ID 失效
- 推荐给家人 → 家人不会用 → 自己变家庭 IT 支持

**Pull**（吸引来 Kaitu）：
- 具体技术数字（26% 丢包 2–5× BBR）替代空泛形容词
- 协议文档公开（`/k2/*` 内容页）
- `k2s` 自托管选项 —— 暗示"我们不藏黑箱"
- 一账号覆盖全家，无需多份订阅
- 中国区 App Store 直接可下载
- k2 vs Reality / Hysteria2 / BBR 的深度技术内容做 SEO + 信任

**Habit**（卡住不换）：
- Clash 配置的肌肉记忆，已调几年
- 机场订阅年费未到期
- "现在的工具大致还能用"
- "VPN 都差不多" 的刻板印象

**Anxiety**（阻止切换）：
- "会不会又是 SS 套壳，一样会挂？"
- "付款会被追踪吗？"
- "付完反而更慢怎么办？"
- "公司跑路怎么办？"（→ 自托管兜底消解）

---

## Customer Language

### 问题描述（工单 + 社交平台原话）

- "连不上了"
- "速度很慢" / "卡" / "特别是晚上卡"
- "挂了" / "失效了" / "被封了"
- "翻不出去"
- "家里老人不会用"
- "一个账号能用几台？"
- "有没有试用？"
- "退款吗？"

### 对我们的期待（aspirational）

- "连得稳"
- "丢包也能用"
- "简单" / "老人也会用"
- "不折腾"

### 必须用的词

**中文：** 稳定 · 高丢包 · 满速 · 隐身 · 零配置 · 5 台设备 · 全球节点 · 即时开通 · 7 天退款

**英文：** stable · high-loss · line-rate · stealth · zero-config · 5 devices · global nodes · instant · 7-day refund

### 禁用词

- **官方文案不直写 "翻墙"**（敏感）→ 用 "科学上网 / 隐身网络 / 全球网络访问"
- 不使用"最快 / 世界第一 / 永远不掉线"这类无证据超级词 → 用基准数字替代
- 不使用 AI 指纹词：首先/其次/最后、值得注意的是、在当今、总的来说、"In conclusion"、"It's worth noting that"、"Here's the thing:"
- 不攻击友商个人 / 公司（"跑路 / 骗子 / 垃圾"禁用）
- 家庭 / 消费者场景不用协议术语（ECH / QUIC / k2cc）—— 术语放到技术内容里

### Glossary

| 术语 | 含义 |
|---|---|
| Overleap | 海外市场的独立品牌；海外所有渠道的唯一品牌呈现 |
| 开途 | 中国市场的独立品牌（读音 kāi tú）—— 中文语境的唯一写法 |
| Kaitu | 「开途」的英文拼写。**仅用于域名 / bundle id（kaitu.io、`io.kaitu`）**；中文正文与海外面均禁用裸词 |
| ~~Kaitu by Overleap~~ | **已作废**（2026-07-14）—— 两品牌完全隔离，无衔接写法 |
| Overleap LLC | 法务文书署名主体；两品牌唯一共用的跨品牌元素（ToS / 隐私政策 / footer） |
| k2 | 隧道协议名（ECH + QUIC/TCP-WS + k2cc） |
| k2cc | 拥塞控制算法（congestion control） |
| k2s | 可自部署的服务端二进制 |
| k2r | OpenWrt 家用路由器硬件产品 |
| GFW | 防火长城 / "the firewall" —— 技术文案用，消费文案软化 |
| 节点 | 服务器 / 出口节点 |
| 机场 | 第三方翻墙订阅服务（Clash 生态俚语） |
| 线路 | 到某个节点的路径 |

---

## Brand Voice

### 双声线（刻意区分的两种语气）

**技术面**（`/` 首页 hero、`/k2/*` 协议文档、`/install`、changelog、blog 技术内容）：
- **自信、基准驱动、克制**
- 例："别人断线，你满速" / "越拥堵，越从容" / "26% 丢包下吞吐量达 BBR 的 2–5×"
- 证据优先于形容词
- 极简设计，深色背景 + 单色强调

**消费 / 家庭面**（`/support`、新手引导、客服工单、家长指南）：
- **温暖、白话、耐心**
- 例："稳定、简单、为家庭设计" / "不需要繁琐的设置"
- 零术语或术语必解释
- 可使用 Overleap 子视觉（海外）或"开途" 中文（国内）

### 统一风格

- 直接，第一人称（"我们不记录" 优于 "日志被严格管理"）
- 具体数字优先
- 无企业套话（"在当今""值得一提""众所周知"一律不用）
- 中文优先（zh-CN 是母版），英文次之

### 品牌人格（3–5 形容词）

**Engineered** · **Calm** · **Candid** · **Respectful** · **Pragmatic**
工程感 · 沉稳 · 坦诚 · 尊重 · 务实

---

## Proof Points

### 可验证的技术指标

- k2cc 在 26% 丢包下：**2–5× BBR/Brutal 吞吐量**（Kaitu 内部 benchmark，`web/content/*/k2/vs-bbr.md`）
- **9/9 协议维度**在对比矩阵中领先（vs WireGuard/VLESS+Reality/Hysteria2/Shadowsocks）：ECH 隐身、TLS 指纹伪装、主动探测防御、QUIC、TCP 降级、拥塞控制、零配置、CT 零暴露、端口复用
- **5 设备**同时在线（basic 档）
- **6 平台**：Windows 10/11、macOS 12+（Intel + Apple Silicon）、Linux、iOS、Android、OpenWrt 路由器
- **中国区 App Store 可直接下载 iOS 客户端**
- **代码签名**：Apple 公证 + Windows EV 签名

### 社会证据（当前现状 + 行动项）

**当前：暂无采集的用户评价 / 推荐语。**

**待落地行动项（不在本文档范围内）：** 建立首次评价采集机制。建议渠道优先级：

1. App Store / Google Play 应用内评分 prompt（付费激活后 N 天触发；通过 `mcp__asc__*` 拉取已有评分做监控）
2. 工单解决后满意度回调
3. 小红书 / Twitter / V2EX 搜索 "kaitu / 开途" 的用户 UGC，授权后引用
4. 所有评价引用前去除可识别个人信息

**在评价采集起来之前，proof 完全依赖技术证据 + GitHub 可见度 + 公开文档。**

### Value Themes

| 主题 | 证据 |
|---|---|
| GFW 下稳定 | k2cc benchmark + ECH SNI + 双栈传输 |
| 非技术用户也会用 | 一键安装 + `/support` 家长指南 + WhatsApp 客服 + USB 辅助安装 |
| 隐私 | 无日志政策 + 加密货币支付 + 自托管兜底 + CT 零暴露 |
| 家庭就绪 | 5 设备 + 成员委托 + OpenWrt 家用路由器 |
| 透明 | 公开协议文档 + GitHub kaitu-io 开源 + 自托管二进制 |

---

## Goals

**商业目标：** 扩大中国市场 `basic` 档付费订阅，为 `lite/family/business` 多档位上线（Spec B）准备。海外（Overleap）处于验证期，保守投入。

**主转化动作：** `/install` 下载客户端 → 首次连接成功（激活）→ `/purchase` 付费订阅。

**次级转化动作：**
- 自托管开发者：`curl kaitu.io/i/k2s | sudo sh`（acquisition 顶部漏斗，不直接付费但建立信任 / SEO / 口碑）
- KOL / 分销兑换码：`/s/[code]` + `/redeem`
- k2r 路由器预售留资：`/routers`

**当前指标基线（填充方式）：**
- DAU：从 `usage_overview` MCP 工具拉取
- 付费用户数：`user_statistics`
- 月度营收：`order_statistics`
- 付费转化率：`order_statistics`
- 试用→付费转化：尚未埋点（TODO）
- 续费率：尚未埋点（TODO）

---

## Strategic Open Questions

以下为不阻塞本文档使用、但需要独立决策的战略问题。团队应逐一回答：

1. ~~**Kaitu ↔ Overleap 品牌关联策略**~~ — ✅ 已决策 (2026-07-14，取代 2026-04-21 的层级方案)：**两品牌完全隔离，无关联、无衔接词**。~~"Kaitu by Overleap"~~ 作废。
2. ~~**Overleap 海外 GitHub org 命名**~~ — ✅ 已决策 (2026-04-21)：`getoverleap`（github.com/getoverleap）
3. **Overleap 法律实体注册**：Inc. / LLC / Ltd. 选型 + 司法辖区（美国 / 新加坡 / 香港 / 开曼）
4. **Overleap 的海外 ICP 锁定**：华人出海 / 隐私技术用户 / 其他审查市场 —— 选哪个做 launch wedge？
5. **family / business tier（Spec B）上线时机 + 定价策略**
6. **首次评价采集机制**：谁负责、在哪触发、如何沉淀
7. **k2r 路由器商业上线时机 + 与 family tier 绑定策略**

---

## Changelog

- **2026-04-21** — V1 草稿由 `marketing-skills:product-marketing-context` skill 在 Kaitu 代码库内基于首页 / install / purchase / support / k2 协议文档 / i18n 源（zh-CN）/ tier-rename spec 自动起草；经 david 澄清品牌结构（Kaitu 中国、Overleap 海外）、ICP 分层（实用派主力）、竞争点名规划后定稿。
- **2026-04-21** — 品牌架构决策：从"双品牌并列"升级为 **"Overleap 母品牌 / Kaitu 中国产品"** 层级结构，衔接词 "Kaitu by Overleap"。连带更新 Brand Architecture / Glossary / Strategic Open Questions。跨市场信任迁移问题由 footer 衔接规则承接；GitHub org 名与法律实体注册转为 Open Questions。详见 `docs/marketing/brand-naming-strategy.md`。
- **2026-07-14** — **品牌架构再次翻转，推翻上一条**：放弃母品牌 / 子产品层级，改为 **开途（中国）与 Overleap（海外）两个完全隔离的对等品牌** —— 各自独立的用户池、支付渠道、叙事，任何面向用户的语境都不互相提及。衔接句 "Kaitu by Overleap" / "Kaitu, a product of Overleap" 作废；"跨市场信任迁移" 作为策略取消（隔离前提下不成立）。唯一保留的跨品牌元素是法务文书署名 **Overleap LLC**。连带更新 Brand Architecture / Glossary / Strategic Open Questions。
- **2026-07-15** — 修复文档漂移：本文档、`docs/marketing/{README,brand-naming-strategy,content-calendar-2026-Q2}.md` 与根 `CLAUDE.md` 此前全部停留在 2026-04-21 的层级架构，07-14 的决策只存在于品牌拆分代码分支中，导致每次 `marketing-skills:*` 启动都加载已被推翻的架构。五处口径已统一到"完全隔离"。
