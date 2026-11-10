---
name: content-roi-report
description: Use when producing the biweekly 开途 content revenue table (每篇文章带来多少新用户和钱) — joins docs/marketing/content-registry.md, Bing page impressions, and the content_conversion / web_install funnels into one row per article and appends it to the growth ledger. Also use when asked "哪篇文章带来了付款 / 内容收益 / content ROI".
---

# 内容收益报告（每两周）

设计与口径：`docs/superpowers/specs/2026-11-10-content-roi-tracking-design.md`。口径固定，**不要临场改**——改了就和上一期不可比；真要改，先改 spec，并在报告里写明「口径变更」。

## 1. 定区间

报告日 T（今天）。进站区间 = **[T−28, T−14)**（共 14 天，转化窗口已走满）。MCP `funnel` 的 `to` 是包含的，所以传 `from = T−28`、`to = T−15`。Bing 用同一区间。

## 2. 取数

全部 `brand: "kaitu"`。

1. `mcp__kaitu-center__funnel(key="content_conversion", group_by="content", inferred=true, from, to)`
   → 每组：`key`、`steps = [进站, 新注册, 付款人数]`、`paidCents`、`listCents`。
2. `mcp__kaitu-center__funnel(key="web_install", group_by="landing_path", ...)` 与 `group_by="source"` → 点下载人数。
3. `mcp__bing-webmaster__bing_site_pages` → 各 `/guides/<slug>` 页面在区间内的曝光、点击（只有站内页面有；Bing 的页面 URL 带语言前缀，按 slug 合并各语言）。
4. 读 `docs/marketing/content-registry.md`（站外文章、站内别名）和 `web/content/zh-CN/guides/*.md` 的 `title`。

## 3. 拼表

`content` 分组键对回文章：
- 形如 `平台/话题` → 登记表站外行；登记表里没有的照列，标「未登记」。
- `/guides/<slug>` → 站内文章，标题取 frontmatter；登记表别名把旧路径并进新路径。
- 其它路径（`/`、`/install`、`/pricing`…）合成一行「非文章页」，不丢。
- 行与行互斥（一个人只在一行），可以出合计行。

点下载：站内文章用 `web_install` 的 landing_path 组对上；站外用 source 组（只能到平台粒度）。**它和转化列不是同一次走查**，表下要注明。

## 4. 写进台账

追加到 `docs/marketing/growth-ledger.md`：

```
### 内容收益 YYYY-MM-DD（进站 YYYY-MM-DD ~ YYYY-MM-DD，含推断归因）

| 内容 | 标题 | 位置 | 曝光 | 进站 | 点下载 | 新注册 | 付款人数 | 金额 | 与上期 |
|---|---|---|---|---|---|---|---|---|---|
```

- **金额**写成 `实付 $x + 订阅标价 $y`（`paidCents` / `listCents` ÷ 100）。两者**不能加成一个数**：订阅按标价算，Apple 有抽成和多币种，不是实收。
- 进站 < 30 标「样本小」。
- 与上期：只比进站、新注册、付款人数；上期没有这一行写「新」。首期写「首期」。
- 报告写进去就是快照，之后的退款不回改。

表下固定写：

- 统计区间、含推断归因。
- 盲区：①读完去 App Store 下载、在 App 里注册的人只能靠推断归因接上一部分，付款偏低；②站外读者没点链接、自己搜「开途」进站的，算不到文章上；③Bing 只是搜索流量的一部分，Google 未接；④首次触达：先直接访问过官网、后来才从文章点进来的人，归给第一次；⑤订阅金额按套餐标价，Stripe 退款未扣。
- 一两句结论：哪些题材有付款、哪些只有曝光没有转化，建议继续写 / 暂停的候选（结论给 David 定，不自己改内容计划）。

## 常见坑

- `funnel` 区间最多 90 天；区间太大报 range too large 时缩短。
- 分组上限：`content` 维度 500 组，超过才出现 `(other)`；其它维度 50 组。
- 后端没部署 `content_conversion` 时 `funnel_paths` 里没有这个 key——先确认，别拿 `guide_conversion` 顶替（它把老用户登录也算进去，口径不同）。
