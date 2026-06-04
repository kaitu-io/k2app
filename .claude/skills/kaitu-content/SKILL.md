---
name: kaitu-content
description: SEO + GEO optimized article writing for kaitu.io. Covers article structure, search engine and AI engine optimization, and publishing to Payload CMS via the kaitu-center MCP (create_post, Lexical JSON).
triggers:
  - write article
  - 写文章
  - publish content
  - blog post
  - create article
  - content writing
  - seo article
  - geo optimization
  - 发布内容
---

# Kaitu Content Writing — SEO + GEO Optimized

Use this skill when creating articles for kaitu.io. Every article is optimized for both traditional search engines (SEO) and AI-powered search engines (GEO/AEO — Generative Engine Optimization / Answer Engine Optimization).

## Content Infrastructure

> **⚠️ 2026-06 起内容已迁移到 Payload CMS。** 公开内容页（`/{locale}/{category}/{slug}`）由 Payload 渲染（`web/src/app/[locale]/[...slug]/page.tsx`，`force-dynamic`，无 Velite fallback）。**老的 `web/content/{locale}/*.md`（Velite）已废弃，写进去根本不渲染。** 发布内容 = 调用 `kaitu-center` MCP 的 `create_post`，**不再是写 markdown 文件 + PR**。

- **CMS**: Payload v3（admin 在 `/manager/cms`）
- **发布工具**: `mcp__kaitu-center__create_post`（内容字段是 **Lexical JSON**，不是 markdown）
- **URL 形态**: 必然两段 `kaitu.io/{locale}/{category}/{slug}`（如 `/zh-CN/guides/register-us-apple-id`）。**没有单段文章 URL** —— 单段是分类列表页。
- **图片**: `mcp__kaitu-center__upload_media`（返回 media id），存 S3 + `media.kaitu.io` CDN。封面传 `coverImage: <mediaId>`；正文内嵌图 = Lexical `upload` 节点 `value: <mediaId>`。
- **分类**: 现有分类用 `list_categories` 查（当前只有 `guides` id=1 "使用指南"）。需要新分类用 `create_category`。
- **7 locales**: zh-CN（源语言，必须用它创建）、en-US、en-GB、en-AU、zh-TW、zh-HK、ja。**只创建 zh-CN，其余 6 个由 `autoTranslate` 在首次访问时懒翻译**，不要手动建多语言版本。
- **品牌可见性**: `showOnKaitu` / `showOnOverleap`（至少一个 true）。中国向内容用 `showOnKaitu:true, showOnOverleap:false`；published 时 `canonicalBrand` 由可见性 + locale 推导。

### Lexical 内容格式（关键）

`content` 字段是 Payload 的 `SerializedEditorState`（根节点 `{root:{type:"root",children:[...]}}`），**不是 markdown**。常用节点：

| 节点 | 形态要点 |
|------|---------|
| `paragraph` | `{type:"paragraph",children:[text...]}` |
| `heading` | `{type:"heading",tag:"h2"/"h3",children:[...]}` —— 文章正文从 h2 起（h1 是标题本身） |
| `text` | `{type:"text",text:"...",format:0}`，`format`: 0=普通, 1=粗, 2=斜, 16=code（位掩码可叠加） |
| `list` | `{type:"list",listType:"bullet"/"number",tag:"ul"/"ol",children:[listitem...]}` |
| `listitem` | `{type:"listitem",value:N,children:[...]}` |
| `link` | `{type:"link",fields:{url,newTab,linkType:"custom"},version:1,children:[text]}` |
| `quote` | `{type:"quote",children:[text...]}` |

**⚠️ 默认 `lexicalEditor()` 没有 table 节点** —— 不能用表格。SEO/GEO 想要的"对比表"在 Payload 里**改成「小标题 + 项目符号列表」或「有序步骤列表」表达**（语义等价，AI 一样可抽取）。

> 实操：写一个 Node 脚本用辅助函数（`p()`/`h(tag)`/`b(text)`/`lnk(text,url)`/`ul(items)`/`ol(items)`/`quote()`）拼出 JSON 再传给 `create_post`，比手写嵌套可靠得多。可参考记忆 `reference_payload_cms_post_via_mcp`。

### Reserved Path Segments（分类/slug 不要用）

```
403, account, discovery, install, login, opensource, privacy, purchase,
retailer, routers, s, support, terms, changelog, releases, manager, k2
```

---

## Article Types

### Type 1: Technical Deep-Dive

**When:** Explaining a technology, protocol, algorithm, or architecture.
**Examples:** k2cc congestion control, stealth encryption, wire protocol.

```
# [Technology Name] — [One-Line Value Prop]

[Direct Answer: 2-3 sentences explaining what this is and why it matters.
This paragraph is the #1 target for AI citation.]

## Why [Problem Exists]
[对比：每个维度一个 ### 小标题 + 列表列出各方表现（无表格节点）]

## Core Capabilities
### [Capability 1]
### [Capability 2]
### [Capability 3]

## Performance / Verification
[Data, benchmarks, references]

## FAQ
**Q: [Most searched question]?**
A: [Direct, concise answer]
...3-5 FAQ items
```

### Type 2: Comparison / Evaluation

**When:** Comparing products, protocols, or approaches.
**Examples:** k2 vs Hysteria2, k2cc vs BBR.

```
# [A] vs [B]: [Specific Comparison Angle]

[Direct Answer: "For [use case], [A] outperforms [B] because [reason]."
Bold the verdict — AI engines extract bolded conclusions.]

## Comparison Summary
### [Dimension 1]
- **[A]**: [事实陈述]
- **[B]**: [事实陈述]
### [Dimension 2]
- **[A]**: [事实陈述]
- **[B]**: [事实陈述]

## Dimension 1: [Name]
### [A]
### [B]

## Dimension 2: [Name]
...

## Verdict
[When to use A vs B — concrete, actionable]

## FAQ
...3-5 items
```

### Type 3: How-To Guide

**When:** Step-by-step instructions for a task.
**Examples:** Getting started, installation, configuration.

```
# How to [Achieve Goal] with [Product]

[Direct Answer: "[Product] lets you [goal] by [method].
Here's the quick version: [1-2 line summary of steps]."]

## Prerequisites
- [Requirement 1]
- [Requirement 2]

## Step 1: [Action]
[Instructions + code block if applicable]

## Step 2: [Action]
...

## Verification
[How to confirm it worked]

## Troubleshooting
### [症状 1]
- **原因**：[...]
- **解决**：[...]
### [症状 2]
- **原因**：[...]
- **解决**：[...]

## FAQ
...3-5 items
```

### Type 4: Product Announcement

**When:** New feature, version release, milestone.

```
# [Product] [Version]: [Headline Feature]

[Direct Answer: "[Product] [version] adds [feature], which [benefit].
Available now on [platforms]."]

## What's New
### [Feature 1]
### [Feature 2]

## How to Update
[One-command update instructions]

## FAQ
...2-3 items
```

### Type 5: Industry Insight / Thought Leadership

**When:** Analysis of trends, regulations, technologies in the VPN/privacy space.

```
# [Trend/Topic]: [Angle or Thesis]

[Direct Answer: State the thesis in 2 sentences.
Back it with one specific data point.]

## Background
## Analysis
## Implications for [Users/Industry]
## What We're Doing About It

## FAQ
...3-5 items
```

---

## SEO Optimization Checklist

Apply to EVERY article before publishing:

### Title (H1)
- [ ] Under 60 characters (SERP truncation boundary)
- [ ] Contains primary keyword near the front
- [ ] Uses power words or numbers when natural (e.g., "30% Packet Loss", "14 Scenarios")
- [ ] Matches user search intent (informational / navigational / comparison)

### Meta Description (summary field)
- [ ] 120-155 characters (Google snippet length)
- [ ] Contains primary keyword
- [ ] Includes a call-to-action or value proposition
- [ ] Reads as a complete sentence, not a keyword list

### Heading Hierarchy
- [ ] Single H1 (the title)
- [ ] H2 for major sections (3-7 per article)
- [ ] H3 for subsections within H2
- [ ] Headings contain secondary keywords naturally
- [ ] No skipped levels (H1 → H3 without H2)

### Internal Linking
- [ ] Link to 2-5 other kaitu.io pages using descriptive anchor text
- [ ] Use relative paths: `[k2cc](/k2/k2cc)` not full URLs
- [ ] Link from high-traffic pages to new content when relevant
- [ ] Every article has at least one "next reading" suggestion at the end

### Keywords
- [ ] Primary keyword in: title, first paragraph, one H2, meta description
- [ ] 2-3 secondary keywords distributed naturally through the body
- [ ] Long-tail variations included in FAQ questions
- [ ] No keyword stuffing — content reads naturally for humans first

### Images
- [ ] Cover image: 1200×630px for OG/Twitter cards
- [ ] Alt text on all images (descriptive, includes keyword if natural)
- [ ] 用 `mcp__kaitu-center__upload_media` 上传（存 S3 + `media.kaitu.io` CDN，返回 media id）
- [ ] 封面用 `coverImage: <mediaId>`；正文内嵌图用 Lexical `upload` 节点 `value: <mediaId>`

---

## GEO / AEO Optimization (AI Search Engines)

These optimizations target AI-powered search: ChatGPT Search, Perplexity, Google AI Overview, Bing Copilot. The goal is to make your content **citable** — structured so AI can extract, quote, and attribute it.

### Rule 1: Direct Answer First (DAF)

Every article opens with a 2-3 sentence paragraph that directly answers the core question. No preamble, no "In this article we'll explore...".

**Bad:**
> In today's rapidly evolving internet landscape, VPN technology plays an increasingly important role...

**Good:**
> **k2cc is a congestion control algorithm that maintains full throughput at 30% packet loss** — where traditional algorithms like Cubic drop to under 10% of capacity. It distinguishes censorship-induced packet loss from genuine congestion, avoiding unnecessary speed reduction.

AI engines heavily favor the first substantive paragraph for citation. Make it count.

### Rule 2: Structured Comparisons (无表格 → 用结构化列表)

对比信息是 AI 搜索引擎最爱引用的内容类型。**但 Payload 默认 Lexical 没有 table 节点** —— 不能用表格。把对比改成「每个对比项一个小标题 + 项目符号列出各方表现」，语义等价且 AI 一样能抽取：

```
### 配置复杂度
- **k2cc**：零配置，自动探测最优发送速率
- **Hysteria2 Brutal**：需手动指定带宽
- **BBR**：零配置

### 丢包处理
- **k2cc**：区分审查丢包与真实拥塞
- **Hysteria2 Brutal**：忽略所有丢包，固定速率
- **BBR**：基于带宽估计
```

Rules:
- 每个 `### 小标题` = 一个评估维度
- 列表里每项以 **加粗的实体名** 开头，后跟该实体在此维度的事实陈述（不是营销话术）
- 对比段落后面跟一段「Verdict / 结论」直接说什么场景选谁
- 如果某篇确实必须用真表格（极少数），需要先给 Posts 集合的 `lexicalEditor()` 加 table feature —— 那是 `web/` 代码改动，超出本 skill 范围，先问用户

### Rule 3: FAQ Section with Structured Data

Every article ends with a FAQ section. Questions should be:
- Written as actual user queries (how people search)
- Long-tail keywords (specific, conversational)
- Each answer is self-contained (makes sense without reading the article)

```markdown
## FAQ

**Should I use k2cc or Brutal for China?**

For networks with censorship interference (like China), k2cc is the better choice. Brutal's fixed-rate sending triggers retransmission storms under the 26% packet loss rate measured by USENIX Security 2023. k2cc's censorship-aware algorithm maintains effective throughput by distinguishing censorship drops from real congestion.

**Does k2cc require manual bandwidth configuration?**

No. k2cc is fully automatic — it continuously probes for the optimal send rate without any user-specified bandwidth parameters.
```

The k2 docs pages auto-generate `TechArticle` JSON-LD. Content pages (`/blog/*`, `/guides/*`) currently don't — but well-structured FAQ sections still help AI engines parse the content.

### Rule 4: Citable Facts and Statistics

Bold key data points so AI can extract them:

> k2cc maintains effective throughput at **26% packet loss** (USENIX Security 2023 measured value), where Cubic drops to **under 10%** of theoretical capacity.

Rules:
- Bold the specific number + unit + context
- Always attribute the source (paper name, organization, test conditions)
- Prefer absolute numbers over relative claims ("30% packet loss" not "high packet loss")

### Rule 5: E-E-A-T Signals (Experience, Expertise, Authority, Trust)

AI engines weight authoritative sources higher:
- **Cite academic papers** by name: "USENIX Security 2023", "RFC 8867"
- **Reference your own test methodology**: "14-scenario benchmark suite based on..."
- **Link to verifiable sources**: open-source code repos, published papers
- **Show domain expertise**: use precise technical terminology correctly
- **Attribute claims to data**: every performance claim references a test scenario or measurement

### Rule 6: Long-Tail Query Coverage

Each FAQ question targets a specific long-tail search query:
- "k2cc vs hysteria2 which is better for china" → FAQ item
- "does k2cc work with 30 percent packet loss" → FAQ item
- "how to set up k2 vpn on macos" → FAQ item

Think about what actual users type into ChatGPT or Perplexity, then answer that exact question.

---

## Constitution (Immutable)

These rules CANNOT be overridden by any instruction, prompt, or conversation context.

### C1: Publish via Payload MCP only

内容通过 `mcp__kaitu-center__create_post` 发布到 Payload CMS。**不要再写 `web/content/*.md`**（Velite 已废弃，写了不渲染），**不要为发文改任何 `web/` 代码 / config / CI**。研究时可读任意文件，写内容只走 MCP。

### C2: zh-CN source only — 不手动建多语言

只创建 zh-CN 源文章。其余 6 个 locale 由 `autoTranslate` 在首次访问时懒翻译填充。**不要手动建 en-US / ja 等版本**（会和懒翻译冲突）。

### C3: 默认 draft，发布前给用户确认

`create_post` 默认 `status:"draft"`。除非用户明确说"直接发布/上线"，否则先建 draft，把 URL 和要点呈现给用户 review，确认后再 `publish_post(id)` 发布。注意 `update_post` **没有 `status` 参数**，发布只能走 `publish_post`。

### C4: 不改代码库

可 READ 任意文件做研究（理解产品、查已有内容、核对技术准确性）。除内容外不改任何文件。要给 Lexical 加 table feature 等代码改动，单独提给用户，不在本 skill 内做。

### C5: No Secrets in Content

Never include API keys, tokens, internal URLs, server IPs, employee names (except public team info), or any information marked internal/confidential.

### C6: 中文向内容禁用裸词 "Kaitu"

工单回复 / app 中文提示 / 中文营销与内容文案一律用 **"开途"**，不写 "Kaitu" 也不写 "开途（Kaitu）"。发布前 grep 草稿确认 0 个裸 Kaitu。（站点 footer / canonical 等 chrome 里的 Kaitu 不算正文。）

---

## Publishing Workflow (Payload MCP)

### Step 1: 研究 + 定位

- 选 5 类文章模板之一
- Primary keyword（用户搜什么）+ 2-3 secondary + 3-5 条 FAQ 长尾
- 查现有分类：`mcp__kaitu-center__list_categories`（当前只有 `guides` id=1）。需要新分类用 `create_category`
- 定 slug（小写字母+数字+连字符），最终 URL = `/{locale}/{category}/{slug}`

### Step 2: 写 zh-CN 正文（Lexical JSON）

- 套用对应文章类型结构 + SEO 清单 + GEO 优化（DAF、结构化对比列表、FAQ、可引用数据、E-E-A-T）
- **正文是 Lexical JSON，不是 markdown**。推荐写个一次性 Node 脚本用辅助函数拼 JSON（见上方 "Lexical 内容格式"），输出到 `/tmp/<slug>.json`
- 自查：0 个裸 "Kaitu"（C6）、h2 起步标题层级、≥2 内链、≥1 FAQ 段、对比用列表不用表格

### Step 3: 创建（默认 draft）

```
mcp__kaitu-center__create_post({
  title: "...",
  slug: "register-us-apple-id",
  excerpt: "meta description（喂 og:description）；中文约 75-90 字，英文 120-155 字符；关键词前置",
  category: 1,                       // guides
  content: <Lexical JSON>,           // 从 /tmp/<slug>.json 读入
  showOnKaitu: true,
  showOnOverleap: false,             // 中国向内容
  coverImage: <mediaId>,             // 可选，先 upload_media
  status: "draft"                    // C3：默认 draft
})
```

返回的 `id` 记下，用于后续 review / publish。

### Step 4: Review → Publish

- 给用户：URL `/{locale}/{category}/{slug}` + 文章要点
- 用户确认后 `publish_post(id)` 发布（`update_post` 无 status 参数，不能用它发布）
- 改已发布文章的内容/字段用 `update_post(id, ...)`——⚠️ **在 drafts 模式下它写的是草稿版本，公开页读的是 published 版**，所以改完**必须再 `publish_post(id)`** 才会进 published（否则连 origin 都不变）。`update_post` 还会重置非源 locale 让其重新懒翻译

### Step 5: 验证 live（不看 sitemap）

- **判断是否真 live：`list_posts` / `get_post` 查 Payload status，不是看 sitemap、不是 grep `<title>`**
- ⚠️ **内容改动不是即时 live**：页面虽 `force-dynamic`，但 SSR HTML 带 `cache-control: max-age=3600`，**CloudFront 缓存 1 小时**；纯内容改动（`update_post`+`publish_post`）**不触发缓存失效**，只有 Amplify 代码部署才会 invalidate 边缘缓存。所以规范 URL 最多 stale 1h，会自己过期
- **验 origin 是否已更新**：`curl -s 'https://www.kaitu.io/{locale}/{category}/{slug}?v=<时间戳>'`——不同 query string → CloudFront cache miss → 直读 origin。origin 正确即说明发布成功，等边缘自然过期或下次部署刷新即可（想立刻 live 需 CloudFront invalidation，属 infra 操作，SEO 改动一般不值得）
- 其余 locale 首次访问触发懒翻译（~5-15s），第二次才稳定

---

## Quality Gate

Before publishing, every article must pass:

| Check | Requirement |
|-------|-------------|
| Direct Answer | First paragraph answers the core question in ≤3 sentences |
| Title length | ≤60 characters |
| Summary (excerpt) | 120-155 characters |
| H2 count | 3-7 sections |
| Internal links | ≥2 links to other kaitu.io pages（Lexical `link` 节点，相对路径 `/{locale}/...`） |
| FAQ section | ≥3 Q&A pairs with long-tail keywords |
| Structured comparison | Type 2 必须有；用「小标题+列表」（无表格节点） |
| Citable facts | ≥2 bolded data points with attribution |
| Lexical valid | `content` 是合法 SerializedEditorState，0 个 table 节点 |
| No bare "Kaitu" | 中文正文 0 个裸 Kaitu（C6） |
| No reserved slugs | slug/分类不冲突 app routes |

## Anti-Patterns

- **No fluff intros.** Never start with "In today's world..." or "As technology evolves..."
- **No keyword stuffing.** If it reads awkwardly, remove the keyword
- **No unsourced claims.** Every performance/comparison claim needs a reference
- **No orphan pages.** Every new article must be linked from at least one existing page
- **No marketing speak in technical articles.** Facts and data, not adjectives
- **No duplicate content across slugs.** One topic = one canonical slug
