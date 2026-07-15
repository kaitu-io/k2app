# 3 个月内容策略与日历 — Kaitu / Overleap

*周期: 2026-04-21 → 2026-07-20（13 周）· Skill: `marketing-skills:content-strategy` · Context source: `.agents/product-marketing-context.md`*

> **品牌架构**（2026-04-21 已对齐）：中国线用 Kaitu（kaitu.io），海外线用 Overleap（overleap.io）；两条线独立站独立账号独立品牌名，不交叉。详见 `docs/marketing/brand-naming-strategy.md`。

---

## 背景 & 起点

现有内容资产：

- `web/content/zh-CN/k2/*`：11 篇中文（index / quickstart / server / client / k2cc / protocol / stealth / hop-ports / vs-bbr / vs-hysteria2 / vs-reality）
- `web/content/en-US/k2/*`：10 篇英文（缺 vs-reality）
- `web/content/*/blog/*`：仅 hello-world 占位 —— **整个 blog 是空的**

**内容缺口：**
- 所有现有内容都服务技术派，**零内容服务付费主力实用派、零内容服务家长型**
- 缺竞品对比：Clash / Shadowsocks / WireGuard / Astrill / ExpressVPN / Mullvad 一个都没有
- 发布节奏爆发式（2/21-2/22 一批、3/6、3/17），没稳定内容机器

---

## 4 个内容支柱

支柱设计原则：**一个支柱服务一个 persona，不要做"通用内容"**。现在的 /k2/* 完全押在支柱 #1，下面 3 个要补起来。

| # | 支柱 | 服务 Persona | JTBD 映射 | 主要渠道 | 当前储备 |
|---|---|---|---|---|---|
| 1 | **协议技术深度** | 技术派 / 自托管派 | "GFW 升级也不挂" / "想自建不想被锁死" | zh: kaitu.io/k2 · V2EX · 知乎 <br> en: overleap.io/docs/k2 · GitHub · Reddit · HN · Twitter | ✅ 11 篇已发 |
| 2 | **场景化生活使用** | 实用派（付费主力 ⭐） | "我每天要用 YouTube/ChatGPT/Gmail" | 小红书 · 知乎 · kaitu.io/blog · Telegram | ❌ 零储备 |
| 3 | **竞品 & 协议横评** | 考虑阶段所有 persona | "我已经有 X，要不要换" | zh: kaitu.io/blog (SEO) <br> en: overleap.io/blog · AI 搜索引用（GEO） | 🟡 3 篇（vs BBR/Hy2/Reality）缺 Clash/SS/WG/Astrill |
| 4 | **家庭 & 非技术使用** | 家长型（未来机会段） | "家里老人也要能用" / "娃在国外" | 小红书 · kaitu.io/support · YouTube | ❌ 零储备 |

外加动态触发层（不在固定日历里，事件触发）：

| # | 触发层 | 触发条件 | 内容模板 |
|---|---|---|---|
| 5 | **事件响应** | GFW 升级 / 竞品挂了 / 苹果政策变 / 新版本发 | 24h 内响应 |

---

## 13 周双轨日历

两条独立分线，不共享周末。**每周 1-2 个核心产出，其他靠 repurposing**。

### Kaitu / zh-CN 线（中国市场）

| Week | 日期 | 核心产出 | 渠道 | 支柱 | Persona | 漏斗 |
|------|------|---------|------|------|---------|------|
| W1 | 4/21-4/27 | **基础设施周**：小红书账号激活 + Twitter @kaitu_io 重启 + 知乎个人号/机构号确认 + Telegram 频道搭建 | — | 基建 | — | — |
| W2 | 4/28-5/4 ⚡劳动节 | 小红书：《去日本旅游前必装的 3 个手机应用（第 2 个真的救命）》 + 知乎：《出国前手机要装什么网络工具？》 | 小红书 · 知乎 | P2 场景 | 实用派 | TOFU |
| W3 | 5/5-5/11 | 博客长文：《2026 年 6 个 VPN 横评：哪个真的在中国能用？》（含 Kaitu/Astrill/LetsVPN/Express/Surfshark/Clash 对比表） | kaitu.io/blog | P3 对比 | 实用派 | MOFU |
| W4 | 5/12-5/18 | 博客：《Clash 开始卡？可能不是你的机场的问题》（讲 GFW 对 SS/V2Ray 的新检测手段） + Twitter thread 拆解 | kaitu.io/blog · Twitter | P1 技术 | 技术派 | TOFU |
| W5 | 5/19-5/25 | 小红书 × 3 短笔记：《留学生在国外怎么看 b 站》《在家爸妈要刷油管怎么办》《YouTube 看不了的真正原因》 | 小红书 | P4 家庭 + P2 场景 | 家长型 + 实用派 | TOFU |
| W6 | 5/26-6/1 ⚡六一 / 临近六四 | 博客：《历年 6 月前后 VPN 连接故障应对指南》+ 预置"稳定线路清单"落地页 | kaitu.io/blog · 分销推送 | P2 场景 | 实用派 | MOFU |
| W7 | 6/2-6/8 | 预留事件响应（六四周边，若 GFW 动作则启动事件模板；无动作则转发布：《家里用路由器翻墙 vs 每台设备装 App，哪个省心》—— k2r 预热） | 根据情况 | — | — | — |
| W8 | 6/9-6/15 | 知乎长答 × 2：《VLESS + Reality 还能用多久？》《WireGuard 在中国为什么不稳定？》 | 知乎 · V2EX | P1 技术 | 技术派 | TOFU |
| W9 | 6/16-6/22 ⚡端午 | 小红书《中秋 / 端午回国前，这几个应用记得提前装》+ 出境反向用 VPN 教程 | 小红书 | P2 场景 | 实用派（海外华人） | TOFU |
| W10 | 6/23-6/29 | 博客：《为什么有些 VPN 白天快晚上卡？—— 聊聊 ISP QoS 的小秘密》+ 小红书软化版 | kaitu.io/blog · 小红书 | P1+P2 | 实用派 | TOFU |
| W11 | 6/30-7/6 ⚡建党节 | 事件响应预留（如有 GFW 动作）；无则：《开途新版 v0.5 核心改进详解》—— 新版本发布借势 | kaitu.io/releases | — | — | — |
| W12 | 7/7-7/13 | 知乎 × V2EX 技术帖：《k2 协议为什么选 ECH 而不是域前置》+ 附 GitHub 源码链接导流 | 知乎 · V2EX · GitHub | P1 技术 | 技术派 + 自托管派 | TOFU |
| W13 | 7/14-7/20 | 博客：《2026 年暑期留学生必备：出国前必做的 5 个数字准备》（考试季 / 留学季借势） | kaitu.io/blog · 小红书 | P2 场景 | 实用派（留学家庭） | TOFU |

### Overleap / en-US 线（海外市场）

海外节奏更慢 —— 每 2 周 1 篇重磅长文，之间靠 Twitter / Reddit micro-post 维持曝光。

| Week | 日期 | 核心产出 | 渠道 | 支柱 | Persona | 漏斗 |
|------|------|---------|------|------|---------|------|
| W1 | 4/21-4/27 | **基础设施**：overleap.io 域名结构确认 + 英文博客分类建好 + @overleap_vpn（或 @getoverleap）Twitter 账号开通 | — | 基建 | — | — |
| W2 | 4/28-5/4 | 博客：**"k2cc vs BBR vs CUBIC under packet loss: a benchmark"**（把现有 vs-bbr.md 升级成可引用的基准文，含数据表、方法论、GitHub repro） | overleap.io/blog · Lobste.rs | P1 | 技术派 | TOFU |
| W3 | 5/5-5/11 | Twitter × 5 tweets（每日 1 条）拆解 W2 的博客；Reddit /r/VPN 和 /r/networking 各一帖 | Twitter · Reddit | 放大 W2 | — | — |
| W4 | 5/12-5/18 | 博客：**"Why ECH matters for censorship circumvention (and why most VPNs don't have it)"** | overleap.io/blog · HN | P1 | 技术派 | TOFU |
| W5 | 5/19-5/25 | Twitter thread + Reddit 《How I self-host my own stealth VPN in 60 seconds》（k2s 引流） | Twitter · Reddit /r/selfhosted | P1 | 自托管派 | TOFU |
| W6 | 5/26-6/1 | 博客：**"Shadowsocks, V2Ray, Xray, VLESS, Reality: which protocol is still worth it in 2026?"**（超长对比，target AI 搜索引擎） | overleap.io/blog · AI 引用优化 | P3 对比 | 技术派 + 考虑期 | MOFU |
| W7 | 6/2-6/8 | Twitter thread 拆 W6；/r/VPN 发缩减版 | Twitter · Reddit | 放大 | — | — |
| W8 | 6/9-6/15 | 博客：**"Overleap vs Mullvad vs IVPN: stealth VPN comparison"**（海外隐私向对比，明点名头部友商） | overleap.io/blog | P3 | 海外隐私用户 | MOFU |
| W9 | 6/16-6/22 | Twitter + HN 发布 k2 的 technical RFC-style spec（本次不求 HN front page，求永久可引用的技术权威） | HN · Twitter · GitHub | P1 | 自托管派 | — |
| W10 | 6/23-6/29 | 博客：**"The VPN you're using might be lying to you about packet loss"**（原创数据 + 挑战性叙事 —— shareable pillar） | overleap.io/blog · Twitter · HN | P1 | 技术派 | TOFU |
| W11 | 6/30-7/6 | 博客：**"How Iran / Russia / China block VPNs differently (and what that means for protocol choice)"**（跨审查市场内容，Overleap wedge #3） | overleap.io/blog · Reddit 地区版 | P1+P3 | 审查市场用户 | TOFU |
| W12 | 7/7-7/13 | Twitter thread 拆 W11 + 向 Iran/Russia 社区 KOL 发 outreach 邮件 | Twitter · 邮件 outreach | 放大 | — | — |
| W13 | 7/14-7/20 | 博客：**"A 3-month retrospective: Overleap's first 100 paying users"**（透明 meta 内容 —— 建立独立个体品牌信任） | overleap.io/blog · Twitter · HN | P2 | 所有 | BOFU |

---

## Content Reuse System（一篇 → 多渠道）

每篇英文长文按这个链路最多产出 9 件二级内容：

```
                    博客长文（主内容，3000-5000 字）
                              |
        ┌─────────┬──────────┬──────────┬──────────┬──────────┐
        ▼         ▼          ▼          ▼          ▼          ▼
    Twitter    Reddit    Hacker       小红书    YouTube    Telegram
    thread    /r/VPN    News         笔记 × 2   短视频      频道
    (8-12条)  主帖       Show HN      (中文简化)  (3-5 min)  推送
        |         |          |          |          |          |
        └── Data/graph 单图卡 → 所有平台复用 ─────────────────┘
                              |
                    GitHub repo（可引用数据）
                              |
                    AI 搜索引用（长尾）
```

每周一篇博客 → 最多 9 件次级内容 → 3 个月可产出约 45 件次级内容，覆盖 6-8 个平台。**执行关键：内容生产阶段就决定拆分，不要事后改**。

**反指纹规则**（小红书 / 知乎）：参考 `.claude/skills/kaitu-growth/SKILL.md` 里的 9 条（变长短句 / 口语化 / 不用结构化列举 / 不均等深度 / 第一人称 / 平台原生格式化 / 每篇变化 / 长度变化 / 避免 AI 词 "首先/其次/最后/值得注意的是"）。

---

## 事件驱动内容模板（不在固定日历里）

每类事件有预置模板，触发后 24 小时内发布。

| 事件类型 | 触发条件 | 内容产出（中 / 英） | 渠道 |
|---|---|---|---|
| **GFW 升级** | DAU 24h 内降 >15% + 社交平台出现"挂了"搜索词爆发 | 中：《今天很多工具都挂了，开途目前正常》小红书 / 知乎<br>英：Twitter thread "Major Chinese censorship upgrade detected" | 全渠道 |
| **竞品倒下** | Astrill/LetsVPN/ExpressVPN 大范围故障 / 被下架 | 中：《X 用户迁移指南：数据安全转移到开途》<br>英："Alternative to [competitor] when it fails" | 博客 + 社交 |
| **苹果政策变化** | App Store 规则更新 / 竞品被下架 | 中：《苹果最新政策对翻墙工具的影响》<br>英："What Apple's policy change means for VPN apps" | 博客 + HN |
| **新版本发布** | 产品发布 | 中：《开途 v0.X 发布：这 3 个改进你会直接感觉到》<br>英："Overleap v0.X: what changed under the hood" | /releases + 博客 + 推 |
| **技术圈热点** | ECH / QUIC / VPN 类 RFC 新动态 / 学术论文 | 中英：技术派评论文章 | zh: kaitu.io/k2 + V2EX · en: overleap.io/docs/k2 + HN |

---

## 指标体系（区分 vanity vs 真实影响）

### TOFU — 漏斗顶（看"够不够多人发现"）

**真实指标：**
- 月有机访问 UV（GA4 `organic`，分 zh-CN / en-US）
- 博客阅读完成率（>70% 算完读）
- AI 搜索引用次数（手动 query ChatGPT / Perplexity / Claude："best stealth VPN"等 20 个关键词，记录是否引用）
- GitHub stars 周增长（技术派领先指标）
- Twitter / 小红书 follower 净增

**Vanity（不要当指标用）：**
- 单篇微博转发数、点赞数（无法转化为安装）
- 博客 PV 绝对值（跳出率不到 30% 才有意义）

### MOFU — 漏斗中（看"发现后有没有兴趣"）

**真实指标：**
- `/purchase` 页面访问（按 referrer 分）
- `/install` 页面访问 → 实际下载转化率
- 邮件订阅 / Telegram 关注者转化
- Referral 链接点击（分销 + 邀请好友两个系统分开看）

### BOFU — 漏斗底（看"钱有没有进来"）

**真实指标：**
- 按首次触达内容归因的新增付费用户（GA4 自定义维度 `first_content_source`）
- LTV / CAC 按渠道切分（内容渠道 CAC 应逼近 $0）
- 特定内容页面的付费转化率
- License key 兑换数（分 source_tag）

### 月度 scoreboard（只看 6 个数字）

每月第 1 天的 `kaitu-growth` 日报里加一段：

```
内容引擎月报
- 博客有机 UV（中/英）：X / X
- GitHub stars 月增：+X
- 新增 Telegram/Twitter follower（净）：X / X
- AI 搜索引用命中率（20 query 中）：X / 20
- 内容归因的付费用户数：X（占月新增付费 X%）
- 最高 ROI 渠道：[渠道]
```

---

## Week 1 可执行清单（2026-04-21 → 04-27）

> 这 13 周任何一周都没内容要发 —— **因为 W1 是基础设施周**，所有后续周的执行质量都取决于 W1 做得扎不扎实。

### 必做（周一至周三）

| 任务 | 责任 | 时长 | 验收 |
|---|---|---|---|
| 1. 小红书账号激活 / 确认状态 | 运营 | 2h | 完成人工登录 + Kaitu MCP `check_login_status` 返回 logged_in |
| 2. Twitter @kaitu_io 账号重启 + 发 3 条预热帖（不求转化，求"这个号活着"） | 运营 | 3h | 3 条帖 ≥500 impressions |
| 3. 知乎账号确认（个人 / 机构）+ 5 个关键问题收藏 | 运营 | 2h | 5 个待答问题清单 |
| 4. Telegram 频道 `@kaitu_io` 建立 + 置顶 k2 协议文档链接 + 发第一条用户 onboarding | 运营 | 1.5h | 频道上线 |
| 5. overleap.io 独立站搭建（**独立站**，不 301、不互链；overleap.io 是海外品牌站，kaitu.io 是中国品牌站，两站完全隔离 —— 2026-07-14 决策）—— 本周先拉起落地页 + 博客骨架 | 工程 | 3h | overleap.io 返回 200，有基础首页 + /blog 空状态 |
| 6. `web/content/{locale}/blog/` 目录初始化 + 博客列表页 UI 检查 | 工程 | 1h | 能访问 `/{locale}/blog` 并看到空状态或 hello-world |
| 7. GA4 自定义维度 `first_content_source` 埋点方案设计（归因链路） | 工程 + 运营 | 2h | 文档出方案，不要求落地 |

### 内容生产（周四至周日）

| 任务 | 责任 | 产出 | 用于 |
|---|---|---|---|
| 8. 写 W2 小红书主笔记：《去日本旅游前必装的 3 个手机应用》完整草稿 | 运营 | 1 篇 + 3 张配图 | W2 发布 |
| 9. 写 W2 知乎长答：《出国前手机要装什么网络工具》 | 运营 | 800-1200 字 | W2 发布 |
| 10. 起草 W2 的 Overleap 英文长文大纲：**"k2cc vs BBR vs CUBIC under packet loss"** | 工程 + 运营 | 大纲 + 数据准备清单 | W2 完稿 |
| 11. 竞品监控清单搭建（Astrill / LetsVPN / ExpressVPN / Mullvad 的 GitHub / Twitter / Reddit keyword alert） | 运营 | 监控清单 doc | 长期事件响应 |

### Week 1 末尾必须完成的"事件响应就绪"

- [ ] GFW 升级响应的中 / 英博客模板各起草一版（占位内容，触发时填真实数据）
- [ ] 竞品倒下响应的迁移指南模板各起草一版
- [ ] 反指纹 checklist 打印出来放在内容生产 workflow 里

**W1 结束的成功标准：** W2 开头不需要再做任何决策，只需要按清单发内容。

---

## 反直觉选择说明

1. **刻意不在 W1 发任何 Kaitu 内容**。新账号连发 3 条营销内容必 ban 或限流。先用 1 周做"正常用户养号" —— Twitter 转发行业新闻、小红书收藏别人的笔记、知乎关注问题。

2. **小红书放在最前面而非 Twitter**。实用派（付费主力）刷小红书的时间 ≫ 刷 Twitter。Twitter 在中国线是"技术派入口"不是"付费主力入口"。

3. **英文线博客走 Lobste.rs 和 HN 而非 Medium / Substack**。目标人群（技术派 + 自托管派）不读 Medium。HN 一次上首页的价值 = 1 年中等流量。

4. **不做 YouTube**（至少前 3 个月）。生产成本太高，ROI 差，留到 W14+ 验证其他渠道之后再说。

5. **W11 发"透明 meta 内容"（"我们的前 100 付费用户"）** —— 对一个早期海外品牌，透明度是最强信任信号。等 Overleap 有 100 付费时就可以写，这篇长期会变成 Overleap 的品牌符号文章。

6. **所有博客文章的结尾不放"立即购买"按钮**。放 "想了解更多 k2 技术？→ GitHub" 或 "体验一下 → 免费试用"。信任还没建立时放付费 CTA 会破坏转化。

---

## 进度跟踪

每周结束时在此处更新（Week 1 后开始）：

### Week 1（2026-04-21 → 2026-04-27）
- [ ] 基础设施任务 1-7
- [ ] 内容生产任务 8-11
- [ ] 事件响应就绪清单
- **完成度**: 0/11
- **阻塞**: —
- **下周 W2 发布就绪**: 待 W1 末尾验收

（后续周随内容发布结果更新）

---

## Follow-ups

- W1 结束后立即 schedule 跑 `marketing-skills:ai-seo` 对现有 `/k2/*` 11 篇内容做 GEO 重写
- `marketing-skills:programmatic-seo` 用于扩展 "k2 vs X" 对比页模板（W3 之后启用）
- `marketing-skills:community-marketing` 用于 Telegram + GitHub + V2EX 社区运营方案（W4-W6 之间启用）
- `marketing-skills:referral-program` 用于深度优化现有 retailer + license key viral loop（季度后半启用）
