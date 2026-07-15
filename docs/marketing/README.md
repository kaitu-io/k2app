# Marketing Docs

Kaitu / Overleap 的营销档案统一放在这个目录。

## 索引

### 基础档（长期有效，所有 skill 自动引用）

- **`.agents/product-marketing-context.md`** ⚠️ 注意在 `.agents/` 下，不在本目录
  品牌 / 定位 / ICP / JTBD / 竞品 / 异议 / 文案声调单一事实源。
  **位置固定不可挪** —— 所有 `marketing-skills:*` 启动时硬编码读 `.agents/product-marketing-context.md` 路径。

- [`brand-naming-strategy.md`](./brand-naming-strategy.md) *(2026-07-14)*
  双品牌隔离规则（开途 / Overleap / 协议层）、禁用组合、SEO 关键词矩阵。

### 策略与日历

- [`content-calendar-2026-Q2.md`](./content-calendar-2026-Q2.md) *(2026-04-21)*
  13 周（2026-04-21 → 2026-07-20）Kaitu zh-CN + Overleap en-US 双轨内容日历、4 支柱、事件模板、W1 可执行清单、指标体系。

### 审查报告（时间点快照）

- [`audits/2026-04-21-purchase-cro.md`](./audits/2026-04-21-purchase-cro.md)
  `/purchase` 页面 CRO 审查 + P0/P1/P2 行动清单 + A/B 测试队列。

- [`audits/2026-04-21-aso.md`](./audits/2026-04-21-aso.md)
  iOS App Store + Google Play ASO 审查 + 关键词策略 + 评价启动计划 + 迁移决策框架。

---

## ⚠️ 已知待对齐冲突

### ✅ 冲突 #1：海外英文品牌 — Resolved (2026-07-14，取代 04-21 方案)

**现行决策**：**开途（中国）与 Overleap（海外）是两个完全隔离的对等品牌。**

- 中国面（kaitu.io、国内社交、中国区 App Store、分销）= **开途** —— 中文正文禁用 "Kaitu" 裸词，"kaitu" 只作域名 / bundle id
- 海外面（overleap.io、全球 App Store / Play、英文社交、技术社区）= **Overleap**
- **两边互不提及，没有衔接句。** 唯一例外是法务文书署名 **Overleap LLC**
- 协议层（k2 / k2cc / k2s / k2r）不属于任一品牌，全球共享

**已作废（2026-04-21 → 2026-07-14 被推翻）**：~~Overleap 母品牌 / Kaitu 中国产品 层级结构~~、~~跨语境衔接句 "Kaitu by Overleap"~~、~~跨市场信任迁移策略~~（隔离前提下不成立）。

**连带更新**（2026-07-15 同步）：`brand-naming-strategy.md`、`.agents/product-marketing-context.md`（Brand Architecture / Glossary / Strategic Open Questions / Changelog）、`content-calendar-2026-Q2.md`、根 `CLAUDE.md` 全部改为隔离口径。`audits/2026-04-21-aso.md` 无需改动（本就无层级表述，快照按原样保留）。

**Open Question**：Overleap 法律实体注册 —— 法务已按 Overleap LLC 署名，但司法辖区（美国 / 新加坡 / 香港 / 开曼）与形态均未定。GitHub org 已定 `getoverleap`。

### ✅ 冲突 #2：Kaitu↔kaitai 歧义 — Resolved (2026-04-21)

由冲突 #1 决策自动解决：海外面统一 "Overleap"，不使用 "Kaitu" 裸词，Google 纠错问题从根上消除。中国面用中文 "开途" 或英文拼写 "Kaitu"（带 kaitu.io 域名自然消歧义），无冲突场景。

### ✅ 冲突 #3：关键技术数字（丢包率）— Resolved (2026-04-21)

**决策**：统一采用 `web/content/*/k2/vs-bbr.md` 的说法 —— **"26% 丢包下 2-5× BBR 吞吐"**。

- 数据源：vs-bbr.md 引用的 USENIX Security 2023 测量数据（26% GFW 概率性丢包）
- `brand-naming-strategy.md` 关键词矩阵已更新（"26% 丢包满速" → "26% 丢包下 2-5× BBR"）
- 消费面 tagline "别人断线，你满速"（kaitu.io hero、App Store subtitle）保留 —— 这不是具体数字声明，而是品牌 slogan，与技术口径不冲突

---

## 工作方式

### 开新 session 时

Claude 会自动读 `CLAUDE.md`（根目录），其中有本目录的索引段落。所以打开新 session 说 "接着做 marketing" / "查一下内容日历"，Claude 能找到这些档案。

或直接告诉 Claude "读 `docs/marketing/` 所有 markdown"。

### 更新档案

| 档案 | 谁更新 | 何时更新 |
|---|---|---|
| `.agents/product-marketing-context.md` | 跑 `marketing-skills:product-marketing-context` skill 让它重新起草 | ICP / 定位 / 核心价值主张有重大变化时 |
| `brand-naming-strategy.md` | 手动编辑 | 品牌决策变化时（上面冲突点解决后必须更新） |
| `content-calendar-2026-Q2.md` | 手动编辑进度跟踪部分 / 重大调整让 `marketing-skills:content-strategy` 重跑 | 每周执行结束时更新进度；需要新的下个季度日历时（Q3 开始）新建文件 |
| `audits/*.md` | 重审查时创建新文件（保留历史 snapshot，不覆盖） | CRO / ASO 重审查时 |

### 新增档案时

命名规范：
- 策略 / 日历型：`<topic>-<period>.md` 如 `content-calendar-2026-Q3.md`
- 审查 / 时点快照：`audits/YYYY-MM-DD-<topic>.md` 如 `audits/2026-07-01-purchase-cro.md`
- 新增后更新本 README 的索引段落 + CLAUDE.md 对应索引

---

## 相关 skill 参考

按使用频率排序（具体见 `.claude/skills/` 和 marketing-skills 插件）：

- `kaitu-growth` — 日常运营执行手册（campaign / EDM / license key / retailer / 社交 / GFW 事件响应）
- `marketing-skills:content-strategy` — 内容日历规划
- `marketing-skills:ai-seo` + `marketing-skills:seo-audit` + `marketing-skills:programmatic-seo` — SEO / GEO 三件套
- `marketing-skills:community-marketing` — 社区运营（Telegram / Discord / GitHub / Reddit）
- `marketing-skills:referral-program` — 推荐 / 分销系统优化
- `marketing-skills:paywall-upgrade-cro` — /purchase 等付费页优化
- `marketing-skills:aso-audit` — App Store / Play Store 审查
- `marketing-skills:social-content` — 社交平台内容生产
- `marketing-skills:customer-research` — 用户访谈 / 工单分析 / VOC
- `marketing-skills:launch-strategy` — 产品发布借势
