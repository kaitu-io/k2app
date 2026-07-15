# 品牌命名与传播策略

*Updated: 2026-07-15 — 记录 2026-07-14 双品牌拆分决策：**Overleap（海外）/ 开途·Kaitu（中国）两品牌完全隔离、对等并列**，取代 2026-04-21 "Overleap 母品牌 / Kaitu 中国产品" 层级架构。修订原因：后端 Phase 1 品牌拆分工程（`docs/superpowers/specs/2026-07-14-brand-split-design.md`）把 `users.brand` 定为终身不变的出生属性、两品牌数据/支付/邮件全链路隔离，产品架构已是两个独立品牌而非母子层级——命名传播策略需与此对齐，"Kaitu by Overleap" 衔接句作废。本文档为终局形态；迁移动作见 content-calendar W1 + ASO audit P1/P2。*

## 品牌架构

Overleap 与 开途 / Kaitu 是**两个完全隔离的对等产品品牌**（2026-07-14 双品牌拆分决策）——不是母子层级，也没有公司层 / 产品层之分：Overleap 面向海外市场，开途 / Kaitu 面向中国市场，任何面向用户的语境都不互相提及。法务文书署名统一 Overleap LLC（公司实体落款，不构成产品名衔接）。协议层 k2 / k2cc 与硬件产品线 k2r 不属于任一品牌，在两个品牌下全球共享。

| 层 | 名称 | 适用场景 |
|------|------|---------|
| **中国品牌** | 开途（kaitu.io） | kaitu.io · 小红书 / 知乎 / 微博 · 中国区 App Store · 国内分销 / 客服 |
| **海外品牌** | Overleap（overleap.io） | overleap.io · 英文 Twitter / Reddit / HN · 英文 press · 全球 App Store / Google Play · 多语种文档 |
| **法务实体（跨品牌）** | Overleap LLC | ToS / 隐私政策公司落款 · 合同署名（仅实体名，不带产品名衔接） |
| **协议层（不属于任一品牌）** | k2（隧道协议）· k2cc（拥塞控制） | 技术白皮书 · GitHub · 开发者内容 · benchmark |
| **硬件产品线（不属于任一品牌）** | k2r（OpenWrt 路由器） | 家用硬件销售（预售期） |

底层是同一套协议栈与代码库 —— 这是**内部事实，不是对外叙事**。

## 核心原则

### 1. 两个品牌完全隔离，互不提及

- **中国面**（kaitu.io / 小红书 / 知乎 / 中国区 App Store / 分销素材）：只出现「**开途**」，不提 Overleap。中文正文**禁用 "Kaitu" 裸词** —— "kaitu" 仅作为域名 / bundle id（kaitu.io、`io.kaitu`）存在。
- **海外面**（overleap.io / 英文 Twitter / Reddit / HN / 全球 App Store / Google Play）：只出现 "Overleap"，不提 Kaitu / 开途（英文用户无背景）。
- **没有跨语境衔接。** ~~"Kaitu by Overleap"~~ / ~~"Kaitu, a product of Overleap"~~ **已作废，禁止使用**（2026-07-14）。英文 press、跨市场技术博客、招聘页一律选定一个品牌叙事，不做关联。**这不是风格偏好，是工程事实的推论**：`users.brand` 是终身不变的出生属性，两品牌用户 / 数据 / 支付 / 邮件全链路隔离，不存在"同一用户看两个品牌"的场景，故也不需要衔接叙事。
- **唯一例外：法务文书署名。** ToS / 隐私政策 / footer 的法律实体落款两边都是 **Overleap LLC** —— 仅实体名，不出现 "Kaitu by Overleap" 式的产品名衔接。这是刻意收窄的单点例外，不要据此在叙事层重建关联。

### 2. 不回避 "VPN" 品类词

产品已被 GFW 封锁，目标用户就是需要翻墙的人。回避 "VPN" 只会把搜索流量送给竞品。

- 中文：**开途VPN**（国内 App Store 合规紧张时可回退 "开途 网络加速"；详见 ASO audit 合规风险段）
- 英文：**Overleap VPN**

### 3. "开途" 中文品牌是干净的，不需要后缀

"开途" 在中文搜索中无实体碰撞。
- 口语 / 简称：开途
- 正式场合：开途（读音 kāi tú）
- 传播 / SEO：开途VPN

### 4. k2cc 必须带技术上下文出现

"k2cc" 缩写与业余无线电呼号 K2CC（Clarkson University ARC）冲突，需要上下文消歧义。
- 技术文档首次提及必须写全称 **k2cc (k2 congestion control)**；后续可简写，但周围须保留"拥塞控制 / congestion control / 协议 / 吞吐 / 丢包"等相关词
- 绝不在无技术上下文处孤立使用 "k2cc"

### 5. 协议层在所有品牌下共享同名

k2 / k2cc / k2s / k2r 是协议与产品线的技术名称，全球统一。不因市场切换改名。
- 中国市场引用时可译为 "k2 隧道协议" / "k2cc 拥塞控制算法"
- 海外市场引用时直接用 "k2 protocol" / "k2cc congestion control"
- GitHub 仓库无论挂在哪个 org 都用 `k2` / `k2s` / `k2r` 命名

## 绝对禁止

- ~~开途加速器~~ — 品类拥挤（UU / 迅游 / 雷神），会被归入游戏加速器
- ~~Kaitu VPN~~（海外英文裸词）— 海外统一 Overleap，不使用 Kaitu 裸词，避免 Google 纠错成 kaitai（Kaitai Struct 二进制解析器）
- ~~开途VPN by Overleap~~ / ~~Kaitu by Overleap~~ / ~~Overleap by Kaitu~~ — **任何方向的品牌衔接都禁止**（2026-07-14 起两品牌对等隔离，谁也不是谁的母品牌）。"Kaitu by Overleap" 曾在 2026-04-21 被指定为正确写法，现已作废。
- ~~k2arc~~ — 已弃用，搜索冲突严重（业余无线电 / 数学公式 / 电竞战队）

## SEO 关键词矩阵

### 中文（Kaitu / 开途，目标 kaitu.io + 国内社交平台）

| 优先级 | 关键词 | 用法 |
|--------|--------|------|
| P0 | 开途VPN | 文章标题、meta description、小红书正文 |
| P0 | 开途 k2 | 品牌词 + 产品页 |
| P1 | k2cc 拥塞控制 | 技术文档、知乎 / V2EX 技术答题 |
| P1 | 26% 丢包下 2-5× BBR | 核心卖点差异化（数据源：`web/content/*/k2/vs-bbr.md` + USENIX Security 2023 测量） |
| P2 | 隐身隧道 | 技术特性描述 |
| P2 | 科学上网 / 翻墙 | **仅外站 SEO 文章**，禁用于产品内 / App Store / 合规素材 |

### 英文（Overleap，目标 overleap.io + 英文社交 / 技术社区）

| 优先级 | 关键词 | 用法 |
|--------|--------|------|
| P0 | Overleap VPN | 品牌词、App Store Name、主站 hero |
| P0 | stealth VPN | 海外定位词（低竞争、高相关、context 原意） |
| P0 | k2cc congestion control | 技术差异化 — 协议层全球通用 |
| P1 | censorship-resistant VPN | 反审查定位词 |
| P1 | line-rate under packet loss | 核心卖点（context 原话） |
| P2 | Hysteria2 alternative | 竞品拦截 |
| P2 | WireGuard alternative for censorship | 竞品拦截 |
| P2 | ECH VPN | 稀缺技术词，长尾占位 |

## 域名 / 渠道映射

| 资源 | 主入口 | 说明 |
|------|--------|------|
| 海外品牌站（英文） | overleap.io | 独立站，W1 起建设；不与 kaitu.io 互链、不做 301 |
| 中国品牌站 | kaitu.io | 保留；国内流量入口 |
| GitHub org | **`getoverleap`**（github.com/getoverleap）| 2026-04-21 创建并确认可用；当前 0 repos，过渡期保持私仓，未来开源内容迁入此 org。协议层归属，不构成品牌关联 |
| 法律实体 | 法务文书署名 **Overleap LLC**（两品牌共用，唯一跨品牌例外）| 实体注册尚未落地 —— 司法辖区（美国 / 新加坡 / 香港 / 开曼）与形态 Inc. / LLC / Ltd. 均待定；过渡期商户收款由现有运营主体承接，不硬绑 "Overleap Inc." |
| iOS App Store（中国区） | 开途（bundle `io.kaitu`，逐步替代 `com.allnationconnect.anc.wgios`）| 不显示 Overleap |
| iOS App Store（全球区） | Overleap VPN（新建 listing / bundle） | 独立 listing，不复用 Kaitu 评价 |
| Google Play（全球） | Overleap VPN（待上架） | 中国无 Play，不涉及 |
| Twitter / X | @kaitu_io（中文）· @overleap_vpn 或 @getoverleap（英文） | 双账号并行 |

## 过渡期动作清单

本文档只记录终局形态。**从当前到终局的具体迁移步骤**分别记在：

- `docs/marketing/content-calendar-2026-Q2.md` W1 基础设施清单（overleap.io 独立站建立 + 英文 Twitter 开通）
- `docs/marketing/audits/2026-04-21-aso.md` P1/P2 清单（全球 App Store Overleap listing + Google Play 上架 + 老 Kaitu bundle 迁移）
- Strategic Open Questions（`.agents/product-marketing-context.md`）：Overleap 法律实体注册路径（司法辖区待定；GitHub org 已定为 `getoverleap`）

Glossary 统一维护在 `.agents/product-marketing-context.md` 的 Glossary 段。
