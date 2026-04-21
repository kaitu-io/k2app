# Content Rebrand + Cross-Domain 301 + QoS Narrative Pivot

*Date: 2026-04-22 · Owner: david · Status: DRAFT · Builds on: [`2026-04-21-overleap-brand-web-design.md`](./2026-04-21-overleap-brand-web-design.md)*

## Context

2026-04-21 的 spec 已经把**品牌基础设施**立起来了：`lib/brands.ts` / `lib/brand-server.ts` / `middleware.ts` 的 host 识别 / `sitemap.ts` / `Velite` schema `brand` 字段。该代码当前在 `website` 分支，生产 `kaitu.io` + `overleap.io` 双 host 部署在 Amplify 同一个 app。

该 spec 解决的是**骨架**（Header/Footer 按 brand 渲染 logo、wordmark、legal name），但**没动文案**。当前首页 FAQ、Hero、Features 区域的 JSON copy 仍大量使用：

1. "Kaitu" 作为品牌主语（zh-CN 应为 "开途"，en-*/ja 应为 "Overleap"）
2. "审查 / GFW / 翻墙 / 防火墙 / 封锁" 等政治化词汇在**首页**出现（政治曝露过高，可能触发内容平台降权或运营商级干扰）
3. `comparisonWithOthers` FAQ item 在首页点名 WireGuard / Shadowsocks / VLESS+Reality / Hysteria2（同样的曝露问题，且产品 vs 协议的层级错位）

以及**路由层的两个未完成项**：

4. 现有 middleware `overleap.io` 上访问 zh-* locale 只是 `307` → **同域** `/en-US`，应该是 `301` → **kaitu.io/{same-locale}`。
5. 现有 middleware **不**拦截 `kaitu.io` 上的 en-US/en-GB/en-AU/ja 路径，应该 301 → `overleap.io/{same-locale}`。
6. `LanguageSwitcher` 只做 `router.replace(path, { locale })` 同域切换，跨品牌 locale（zh ↔ en/ja）时应构造跨域绝对 URL。
7. `ja` 当前 brand 归属不明确 —— 2026-04-21 spec 的 `OVERLEAP.allowedLocales = ['en-US','en-GB','en-AU']` 没把 ja 放进去，而 `kaitu.io` 又不实际服务 ja 用户。按 2026-04-22 与用户对齐，**ja → overleap.io**。

## Goals

1. **首页内容品牌正确**：首页 Hero / Features / FAQ 的 "Kaitu" 字样按 locale 规则替换（zh → 开途 / 非中文 → Overleap）。
2. **首页政治曝露降低**：首页外露文案里"审查/GFW/翻墙/封锁/防火墙/对抗审查"等词汇替换成"运营商 QoS / 跨境访问 / 网络干扰 / DPI 检测设备"等中性技术术语。
3. **协议对比层级对齐**：`comparisonWithOthers` FAQ item 从首页 FAQ 移走，改到 `/k2/comparison` Velite 页作为**协议对协议**的技术对比（k2 vs WireGuard/Shadowsocks/VLESS+Reality/Hysteria2），用 `<table>` 结构化，带 `FAQPage` + `BreadcrumbList` JSON-LD。
4. **跨域 301 SEO redirect**：Locale 与 Brand 不匹配时（kaitu.io/en-US / overleap.io/zh-CN 等）middleware 发 `301` 永久重定向到正确 brand，搜索引擎权重合并到目标 host。
5. **跨域语言切换**：`LanguageSwitcher` 在目标 locale 归属不同 brand 时构造 `https://{target-brand.baseUrl}/{locale}{path}` 的绝对 URL 并触发跨域跳转（`window.location.assign`，不走 next-intl router）。
6. **ja 归属落地**：`brands.ts` 的 `OVERLEAP.allowedLocales` 加 `'ja'`，`KAITU.allowedLocales` 移除 `'ja'`。

## Non-Goals

- 不修改 2026-04-21 spec 已经建立的 Header / Footer / sitemap / robots / Velite schema 架构（只改这些文件中与本 spec 目标直接相关的行）。
- 不重写 `/install` / `/purchase` / `/account` / `/wallet` / `/support` 的详细内容 —— 只做品牌词替换和 QoS 术语扫尾（PR-3）。
- 不修改 Payload CMS 内容（Payload `posts` 集合当前几乎空，用户自行按品牌录入即可）。
- 不做 `/k2` 已有文档的 QoS 术语替换 —— `/k2` 是面向技术用户的深度文档，允许保留"censorship-aware congestion control"等原始技术表述（也是学术文献里的标准说法）。QoS pivot 只针对**首页外露文案**。
- 不修改 k2 协议 / k2cc 算法本身或其技术名称。
- 不做 `overleap.io` 独立 OG image / logo 重新设计（placeholder 够用）—— 留给 2026-04-21 spec 的 Phase 2。

## Architecture

### 1. 术语替换映射（规范表）

| 原词 (zh) | 替换 (zh) | 原词 (en) | 替换 (en) | 原词 (ja) | 替换 (ja) |
|-----------|-----------|-----------|-----------|-----------|-----------|
| 审查 / 审查网络 | 运营商 QoS / 高丢包网络 | censorship / censored networks | ISP QoS / throttled networks | 検閲 / 検閲ネットワーク | ISP QoS / 高損失ネットワーク |
| 审查感知 | QoS 感知（或丢包区分）| censorship-aware | QoS-aware (or loss-aware) | 検閲認識 | QoS 認識 |
| 审查基础设施主动丢弃 | 运营商流量管理设备主动丢弃 | censorship infrastructure actively dropping | ISP traffic management actively dropping | 検閲インフラが能動的に破棄 | ISP トラフィック管理が能動的に破棄 |
| GFW 26% 丢包率 | 高丢包运营商网络（26% 丢包率） | GFW's 26% loss rate / GFW conditions | restricted-network conditions (26% packet loss) | GFW の 26% パケットロス | 高損失ネットワーク条件（26% パケットロス）|
| GFW 下几乎无法使用 | 高限速网络下几乎无法使用 | nearly unusable under GFW | nearly unusable under restricted networks | GFW 下では実用困難 | 高制限ネットワーク下では実用困難 |
| 翻墙 / 翻墙协议 | 跨境访问 / 跨境加速协议 | circumvention / anti-censorship protocol | cross-border access / cross-border protocol | 検閲回避 / 検閲回避プロトコル | クロスボーダーアクセス / クロスボーダープロトコル |
| 防火墙 | 网络中间设备 / DPI 检测设备 | firewall (in GFW context) | network middleboxes / DPI inspection | ファイアウォール | ネットワーク中間機器 / DPI |
| UDP 封锁 | UDP 干扰 / UDP 阻断 | UDP blocking | UDP interference / UDP blocking | UDP ブロック | UDP 干渉 / UDP ブロック |
| 对抗审查 | 对抗限速 / 对抗干扰 | counter censorship | counter throttling / counter interference | 検閲に対抗 | 帯域制限に対抗 |

**例外 — 保留原词的场景：**

- **`/k2/*` Velite docs** 允许保留学术语境下的 `censorship-aware congestion control` / "under GFW conditions"（这是技术社区和论文的标准术语，搜索此类技术关键词的用户期望看到原词；且 `/k2` 深度文档不是 AI Overview / Perplexity 的高频入口，政治曝露风险低）。
- **`benchmark` 场景描述**：可以保留 "simulating 26% packet loss environment"（技术中性），但不能说 "simulating GFW"。
- **新建的 `/k2/comparison.md`**：协议对协议的技术文档，允许原词。
- **Footer legal name / Terms 页**：`Kaitu LLC` / `Kaitu by Overleap` 保持（品牌法律名，不翻）。

### 2. 协议对比迁移到 `/k2/comparison`

**文件结构：**

```
web/content/zh-CN/k2/comparison.md   (新建)
web/content/zh-TW/k2/comparison.md   (新建)
web/content/zh-HK/k2/comparison.md   (新建)
web/content/en-US/k2/comparison.md   (新建)
web/content/en-GB/k2/comparison.md   (新建 — 可复用 en-US 内容)
web/content/en-AU/k2/comparison.md   (新建 — 可复用 en-US 内容)
web/content/ja/k2/comparison.md      (新建)
```

**frontmatter 规范：**

```yaml
---
title: "k2 与主流协议技术对比"         # (各 locale 本地化)
description: "k2 对比 WireGuard / Shadowsocks / VLESS+Reality / Hysteria2 的 9 个关键技术维度"
order: 50
section: "comparison"
---
```

**内容结构：**

1. 一段 intro（1-2 句），直接回答"这些协议有什么区别"，供 AI Overview / Perplexity 抽取（CLAUDE.md GEO rule: "Direct answers first"）。
2. **`<table>` 对比矩阵**（不是 CSS grid，不是无序列表）—— 9 列：ECH / TLS 指纹 / 主动探测防御 / QUIC / TCP 降级 / 拥塞控制 / 零配置 / CT 日志暴露 / 端口复用。5 行：k2 / WireGuard / Shadowsocks / VLESS+Reality / Hysteria2。
3. 每个协议单独一小节（`<h3>`），展开文字说明（迁移自原 FAQ answer 的内容）。
4. `FAQPage` JSON-LD schema（题目"k2 和 WireGuard 有什么区别?"等四问各映射到小节），由页面组件注入。

**侧栏排序：** 现有 `/k2` 侧栏按 `order` + `section` 分组（`getK2Posts(locale)`）。`order: 50` 放在现有技术文档后面、实操文档前面（先查现有 order 分布再精确对齐）。

### 3. 首页 FAQ 结构改动

**`web/src/app/[locale]/page.tsx` 的 FAQ 数组（当前 L63-89）：**

- 删除 `"comparisonWithOthers"` 项。
- 其余 18 项保留，key 名不变。
- 可选加一项 `"qosThrottling"`（替代被移除项的位置，讲"为什么在运营商限速下还能稳"），但**非必须** —— 现有 `gfwSpeed` rename 为 `networkThrottlingSpeed` 后已覆盖该叙事。

**`gfwSpeed` key rename：** 从 `gfwSpeed` → `networkThrottlingSpeed`（7 个 locale 的 JSON 同步改 key name；page.tsx L63-89 的数组同步改）。这是**整块改名 + 改内容**，不保留 alias（按 `feedback_no_defensive_migration_bridges.md`：不加迁移兼容桥）。

### 4. 跨域 301 Redirect 逻辑

**`web/src/middleware.ts` 修改点：**

现有逻辑（当前分支）：

```ts
// overleap.io + zh-*|ja path → 307 /en-US (same domain)
if (brand.id === 'overleap') {
  const match = pathname.match(/^\/(zh-CN|zh-TW|zh-HK|ja)(\/.*)?$/);
  if (match) {
    return NextResponse.redirect(new URL(`/en-US${rest}`, request.url), 307);
  }
}
```

改为（双向 + 301 + 保留 locale）：

```ts
// 规则表 — locale → brand ownership
function ownerBrand(locale: string): BrandId {
  if (locale === 'zh-CN' || locale === 'zh-TW' || locale === 'zh-HK') return 'kaitu';
  return 'overleap'; // en-US/en-GB/en-AU/ja
}

// 1. 识别当前 host 的 brand 和 path 里的 locale
const brand = brandFromHost(host);
const localeMatch = pathname.match(/^\/(zh-CN|zh-TW|zh-HK|en-US|en-GB|en-AU|ja)(\/.*)?$/);

if (localeMatch) {
  const pathLocale = localeMatch[1];
  const rest = localeMatch[2] ?? '';
  const target = ownerBrand(pathLocale);

  // 2. Brand 不匹配 → 301 跨域 redirect（仅生产域名，localhost 跳过）
  if (target !== brand.id && isProductionHost(host)) {
    const targetBrand = target === 'kaitu' ? KAITU : OVERLEAP;
    const targetUrl = `${targetBrand.baseUrl}/${pathLocale}${rest}${search}`;
    return NextResponse.redirect(targetUrl, 301);
  }
}
```

- `isProductionHost(host)` 判断：仅当 host 是 `kaitu.io` / `www.kaitu.io` / `overleap.io` / `www.overleap.io` 时启用跨域 redirect；`localhost` / `127.0.0.1` / preview 域名 / Amplify staging 域名**不**跨域（开发和预览环境继续同域切 locale，否则 dev 打不开）。
- `search`（query string）保留。
- 搜索引擎 crawler 拿到 `301` + `Location: https://other-brand.io/...`，权重永久合并到目标 host。

**Root path `/` 的行为保持不变**（沿用 2026-04-21 spec 的逻辑：overleap.io → 307 /en-US；kaitu.io → Accept-Language 检测）。但 `/` root 的 redirect **不跨域**（两个 host 的根都指向自己的默认 locale），因为 root 上没有 locale 信息可用来判断应该跨到哪边。

### 5. LanguageSwitcher 跨域

**`web/src/components/LanguageSwitcher.tsx` 修改：**

当前：

```tsx
router.replace(pathname, { locale: newLocale });
```

改为：

```tsx
const currentBrand = useCurrentBrand(); // 新 hook，从 context 或 client-side 读 x-brand
const targetBrand = ownerBrand(newLocale);

if (targetBrand === currentBrand.id || !isProductionHost(window.location.host)) {
  // 同 brand 或本地开发 → 现有路径切 locale
  router.replace(pathname, { locale: newLocale });
} else {
  // 跨 brand → 绝对 URL 硬跳
  const targetBaseUrl = targetBrand === 'kaitu' ? KAITU.baseUrl : OVERLEAP.baseUrl;
  window.location.assign(`${targetBaseUrl}/${newLocale}${pathname}${window.location.search}`);
}
```

- `useCurrentBrand()` — 新建 client hook，读由 RSC layout 注入到 `<html data-brand>` 属性的值（实现细节：在 root layout 的 `<html>` 元素加 `data-brand={brand.id}`，client hook 用 `document.documentElement.dataset.brand` 读）。
- 同域切换和跨域跳转的 UX 差异可接受（跨域会闪一下，但是 locale 切换天然就是重渲染场景，用户可感知）。

**Dropdown 显示规则保持现状**：7 个 locale 全列出，不因为当前 host 过滤。用户在 overleap.io 上点 "简体中文" 就能跳去 kaitu.io/zh-CN。

### 6. `brands.ts` locale 归属更新

```ts
// 现状
OVERLEAP.allowedLocales = ['en-US', 'en-GB', 'en-AU'];
KAITU.allowedLocales = ALL_LOCALES; // 7个

// 改为
OVERLEAP.allowedLocales = ['en-US', 'en-GB', 'en-AU', 'ja'];
KAITU.allowedLocales = ['zh-CN', 'zh-TW', 'zh-HK'];
```

连带 `middleware.ts` 的 overleap.io redirect 正则从 `/^\/(zh-CN|zh-TW|zh-HK|ja)/` 改成 `/^\/(zh-CN|zh-TW|zh-HK)/`（ja 不再被 overleap.io 拒绝）。

## File-by-file Changes

### PR-1: Content Rebrand + QoS Pivot + Comparison Move

| 文件 | 改动 |
|------|------|
| `web/messages/zh-CN/hero.json` | 按术语表批量替换；删除 `faq.items.comparisonWithOthers`；`gfwSpeed` 整 key rename 成 `networkThrottlingSpeed`（含内部 question/answer 改写）；"Kaitu" → "开途"；例外 `Kaitu by Overleap` 保留 |
| `web/messages/zh-TW/hero.json` | 同上 |
| `web/messages/zh-HK/hero.json` | 同上 |
| `web/messages/en-US/hero.json` | 按英文术语表替换；删 `comparisonWithOthers`；`gfwSpeed` → `networkThrottlingSpeed`；"Kaitu" → "Overleap" |
| `web/messages/en-GB/hero.json` | 同 en-US |
| `web/messages/en-AU/hero.json` | 同 en-US |
| `web/messages/ja/hero.json` | 按日文术语表替换；删 `comparisonWithOthers`；`gfwSpeed` → `networkThrottlingSpeed`；"Kaitu" → "Overleap" |
| `web/src/app/[locale]/page.tsx` | FAQ 数组（L63-89）删 `"comparisonWithOthers"` 项；`"gfwSpeed"` 改 `"networkThrottlingSpeed"` |
| `web/content/zh-CN/k2/comparison.md` | 新建 — 协议对比内容（Velite markdown） |
| `web/content/zh-TW/k2/comparison.md` | 新建 |
| `web/content/zh-HK/k2/comparison.md` | 新建 |
| `web/content/en-US/k2/comparison.md` | 新建 |
| `web/content/en-GB/k2/comparison.md` | 新建 |
| `web/content/en-AU/k2/comparison.md` | 新建 |
| `web/content/ja/k2/comparison.md` | 新建 |

**预计 diff：~400 行（7 locale × ~20 处 JSON 改 + 7 个新 markdown 文件）。**

### PR-2: Cross-Domain Routing

| 文件 | 改动 |
|------|------|
| `web/src/lib/brands.ts` | `OVERLEAP.allowedLocales` 加 `'ja'`；`KAITU.allowedLocales` 改为 `['zh-CN','zh-TW','zh-HK']`；新增 `ownerBrand(locale)` helper |
| `web/src/lib/host-utils.ts` | 新建 — `isProductionHost()` 判断 |
| `web/src/middleware.ts` | 替换 `overleap.io` 的 zh-* 307 逻辑为双向 301 跨域；localhost bypass |
| `web/src/app/[locale]/layout.tsx` | `<html>` 元素加 `data-brand={brand.id}` 供 client 读 |
| `web/src/hooks/useCurrentBrand.ts` | 新建 client hook — 读 `document.documentElement.dataset.brand` |
| `web/src/components/LanguageSwitcher.tsx` | 目标 locale 跨 brand 时走 `window.location.assign` 绝对 URL |
| `web/src/app/sitemap.ts` | 已按 2026-04-21 spec 做 brand-aware；确认仍遵循更新后的 `allowedLocales`（ja 从 Kaitu 移到 Overleap） |
| `web/src/components/HreflangLinks.tsx` | hreflang 按 `ownerBrand(locale).baseUrl` 生成绝对 URL（跨域 hreflang）；`x-default` → `https://kaitu.io/zh-CN` |
| `web/tests/lib/brands.test.ts` | 补 `ownerBrand()` 单测 |
| `web/tests/middleware.test.ts` | 新建（或扩充现有）— kaitu.io/en-US → 301 overleap.io/en-US；overleap.io/zh-CN → 301 kaitu.io/zh-CN；localhost 同域 |
| `web/tests/e2e/cross-domain-redirect.spec.ts` | 新建 Playwright E2E — 用 `request.Host` header override 验证 301 Location |

**预计 diff：~250 行（含测试）。**

### PR-3: Site Copy Sweep

扫描剩余位置的残余 Kaitu + 审查/GFW/翻墙词：

| 范围 | 文件 |
|------|------|
| 其他 i18n namespace | `web/messages/{locale}/{install,support,discovery,purchase,wallet,guide-parents,hero-missing,...}.json` — 任何剩余 "Kaitu" 品牌词和审查术语 |
| Velite content | `web/content/{locale}/*.md`（**不含** `/k2/*`，k2 docs 保留原词） |
| React 组件硬编码字符串 | `web/src/components/**` + `web/src/app/**` 里搜 `/\bKaitu\b/` |

**预计 diff：~200 行。**

## Testing Strategy

### PR-1

- `cd web && yarn build` 必须通过（Velite build 不报错，TypeScript 严格类型不报错）。
- `cd web && yarn test` —— 现有 vitest（含 hero page render + sitemap include k2/comparison）。
- **新增 vitest：** `tests/messages-integrity.test.ts` —— 用 grep 校验 zh-* hero.json 不含 `"Kaitu"`（例外：`"Kaitu by Overleap"` 允许）；en-*/ja hero.json 不含 `"kaitu"` (case-insensitive)；所有 hero.json 不含 `"审查"` / `"GFW"` / `"翻墙"` / `"封锁"` / `"防火墙"` / `"censorship"` / `"circumvention"` / `"検閲"` 。
- **手测：** `yarn dev`，打开 `http://localhost:3000/zh-CN` + `/en-US` + `/ja`，肉眼检查首页 Hero + Features + FAQ 已无 "Kaitu"（zh）/ "kaitu"（en/ja）/ 审查相关词。
- **Playwright smoke：** 现有 E2E 跑绿。

### PR-2

- **Middleware 单测（扩展 2026-04-21 spec 的现有 test）：**
  - host=kaitu.io + path=/en-US/install → 301 https://overleap.io/en-US/install
  - host=kaitu.io + path=/en-GB/purchase → 301 https://overleap.io/en-GB/purchase
  - host=kaitu.io + path=/ja/k2 → 301 https://overleap.io/ja/k2
  - host=kaitu.io + path=/zh-CN/install → 200 pass-through
  - host=overleap.io + path=/zh-CN/blog → 301 https://kaitu.io/zh-CN/blog
  - host=overleap.io + path=/zh-TW/support → 301 https://kaitu.io/zh-TW/support
  - host=overleap.io + path=/zh-HK/ → 301 https://kaitu.io/zh-HK/
  - host=overleap.io + path=/ja/k2 → 200 pass-through（ja 现在归 Overleap）
  - host=overleap.io + path=/en-US/blog → 200 pass-through
  - host=localhost:3000 + path=/en-US/install → 200 pass-through（不跨域）
  - host=preview-123.d3q8wll74rs94h.amplifyapp.com + path=/en-US/install → 200 pass-through（preview 不跨域）
- **Query string / fragment 保留测试：** kaitu.io/en-US/purchase?ref=abc → 301 overleap.io/en-US/purchase?ref=abc
- **LanguageSwitcher 组件测试：**
  - 当前 brand=kaitu，pathname=/zh-CN/install，click "English (US)" → `window.location.assign('https://overleap.io/en-US/install')`（mock `window.location.assign`）
  - 当前 brand=kaitu，pathname=/zh-CN/install，click "繁體（HK）" → `router.replace('/install', { locale: 'zh-HK' })`（同 brand）
  - host=localhost:3000 → 跨 brand locale 也走 `router.replace`（不跨域）
- **E2E（Playwright + Host header override）：**
  - 模拟 `Host: kaitu.io` 请求 `/en-US/install` → 响应 301 + Location `https://overleap.io/en-US/install`
  - 模拟 `Host: overleap.io` 请求 `/zh-CN/install` → 响应 301 + Location `https://kaitu.io/zh-CN/install`
- **Redirect loop 防御测试：** 一次重定向后的目标 URL 不应再触发 redirect（在 staging/生产可以用 curl 验证 `curl -I -L --max-redirs 2`）。

### PR-3

- `yarn build` + `yarn test` 绿。
- 扩展 PR-1 的 `messages-integrity.test.ts` —— 把扫描范围从 `hero.json` 扩展到所有 `messages/**/*.json`（例外 legal namespace 的"Kaitu by Overleap"）。
- 手测 `/install` / `/support` / `/purchase` 三个主要页面在 zh-CN / en-US / ja 下的品牌词正确。

## Rollout

### 顺序

1. **PR-1 Content** 先 merge（零路由副作用；merge 后生产的 Hero + FAQ 立即按 locale 正确显示品牌；`/k2/comparison` 可被搜索引擎索引）。
2. **PR-2 Routing** 第二个 merge（需要完整 middleware + switcher 测试覆盖；Amplify preview 环境用 Host header override 冒烟）。
3. **PR-3 Sweep** 最后 merge（纯文案修补，低风险）。

每个 PR 独立 review、独立部署、独立回滚。

### Rollback

- PR-1 回滚：`git revert` 恢复 JSON + 删除 `/k2/comparison/*.md`；无数据或路由残留。
- PR-2 回滚：`git revert` 恢复 middleware；301 一旦被搜索引擎 cache，短期内 kaitu.io/en-US 会继续被误导到 overleap.io，但 overleap.io 上该页仍然渲染，不 404。部署前确认 PR-1 已上线（否则回滚 PR-1 可能让 overleap.io 上看到 Kaitu 品牌词）。
- PR-3 回滚：纯 revert，无副作用。

### 监控

- PR-2 merge 后 24h 内看 Amplify access log 的 301 比例 —— 突增是预期，突增到异常规模（比如 > 20% 流量）说明搜索引擎爬虫还在抓旧 URL，需要等 `sitemap.ts` 的新 URL 被收录（预计 1-2 周）。
- Google Search Console `Coverage` 报告看有无 "Alternate page with proper canonical tag" 或 "Redirect" 类错误。

## Known Limitations / Risks

- **SEO 合并需时间：** 301 对搜索引擎是永久信号，但 PageRank / 索引合并需要 2-8 周；期间可能出现 kaitu.io/en-US/install 在搜索结果里的排名暂时下滑（因为索引切换中）。风险可接受。
- **`ja` 历史 URL：** 用户可能已经收藏了 `kaitu.io/ja/...` 链接。PR-2 merge 后这些链接会被 301 到 overleap.io/ja/... —— 行为符合预期（链接仍然可用），但用户体验上会有一次品牌切换。
- **Amplify preview 环境不跨域：** `isProductionHost` 只认两个生产 host；feature branch 的 preview URL（`*.amplifyapp.com`）看到的仍是混合内容。**可接受**（preview 本来就不是给外部用户用的）。
- **LanguageSwitcher hook 依赖 `<html data-brand>`：** 如果 SSR 时 brand 没注入（比如 fallback 到默认路由），`useCurrentBrand()` 读到空字符串 —— 此时 fallback 到 `brandFromHost(window.location.host)`。测试需覆盖此 fallback。
- **`x-default` hreflang 固定指向 `kaitu.io/zh-CN`：** 这是因为中文市场是主业务，Google 无法确定用户所在区域时 default 给中文用户。Overleap 市场成为主业务后可以反转，但目前保守选择。
- **`/k2/comparison` 路由同时在 kaitu.io 和 overleap.io 可见：** 这是故意的（协议对比是 brand-neutral 技术内容）；Velite post 可选加 `brand: 'both'`，`canonicalBrand: 'overleap'`（英文原版主场）。中文版 canonical 指向 `kaitu.io/zh-CN/k2/comparison`（locale 本地优先）。

## Open Questions

1. **`/k2/comparison` 是否同时在两个 host 的侧栏显示？** 当前假设是（brand-neutral），但可以选择 `brand: 'kaitu'` 只在中文 host 显示中文版、`brand: 'overleap'` 只在英文 host 显示英文版。按 Velite 现有 filter 逻辑，**两 host 都显示是默认行为**。保持默认。
2. **PR-1 要不要同时加一个新 FAQ item `qosThrottling`（"为什么运营商限速时还稳"）替代 `comparisonWithOthers` 的位置？** 我倾向**不加** —— `networkThrottlingSpeed`（原 `gfwSpeed` 的改名）已经覆盖了这个叙事，避免 FAQ 膨胀。
3. **`Chatwoot` 客服 widget 是否有"Kaitu"品牌词硬编码？** 探索阶段没查。PR-1 实现时顺手 grep 一下 `ChatwootWidget.tsx`，若有则一并改。
4. **中文 "Kaitu" → "开途" 例外白名单是否完整？** 目前只有 `Kaitu by Overleap`（footer legal）和 `Kaitu.io`（wordmark，brand-aware 已处理）。若 PR-1 实施时发现其他需要保留 "Kaitu" 字样的场景（如历史 release note 的产品代号），单独列出再决定。

## References

- `docs/superpowers/specs/2026-04-21-overleap-brand-web-design.md` — 基础设施 spec（brand config / host-aware routing / Header / Footer / sitemap / Velite schema）
- `docs/marketing/brand-naming-strategy.md` — 品牌分层决策
- `.agents/product-marketing-context.md` — ICP / JTBD / 术语
- `CLAUDE.md` SEO & GEO Constitutional Rules — 约束 FAQ / 对比表 / canonical / hreflang 的实现
- `web/CLAUDE.md` — next-intl / Velite / middleware 约束

## Implementation Checklist

### PR-1 (Content)

- [ ] `web/messages/zh-CN/hero.json` — 品牌词 + 术语替换 + `comparisonWithOthers` 删除 + `gfwSpeed` rename
- [ ] `web/messages/zh-TW/hero.json` — 同上
- [ ] `web/messages/zh-HK/hero.json` — 同上
- [ ] `web/messages/en-US/hero.json` — 同上（英文术语表）
- [ ] `web/messages/en-GB/hero.json` — 同上
- [ ] `web/messages/en-AU/hero.json` — 同上
- [ ] `web/messages/ja/hero.json` — 同上（日文术语表）
- [ ] `web/src/app/[locale]/page.tsx` — FAQ 数组改 key
- [ ] `web/content/zh-CN/k2/comparison.md` — 新建，含 `<table>` + FAQPage JSON-LD 结构
- [ ] `web/content/zh-TW/k2/comparison.md` — 新建
- [ ] `web/content/zh-HK/k2/comparison.md` — 新建
- [ ] `web/content/en-US/k2/comparison.md` — 新建
- [ ] `web/content/en-GB/k2/comparison.md` — 新建
- [ ] `web/content/en-AU/k2/comparison.md` — 新建
- [ ] `web/content/ja/k2/comparison.md` — 新建
- [ ] `web/tests/messages-integrity.test.ts` — 新建
- [ ] `yarn build` + `yarn test` 绿
- [ ] 手测三个代表 locale 的首页

### PR-2 (Routing)

- [ ] `web/src/lib/brands.ts` — `allowedLocales` 更新 + `ownerBrand()` helper
- [ ] `web/src/lib/host-utils.ts` — 新建 `isProductionHost()`
- [ ] `web/src/middleware.ts` — 双向 301 跨域逻辑
- [ ] `web/src/app/[locale]/layout.tsx` — `<html data-brand>`
- [ ] `web/src/hooks/useCurrentBrand.ts` — 新建
- [ ] `web/src/components/LanguageSwitcher.tsx` — 跨域跳转
- [ ] `web/src/components/HreflangLinks.tsx` — 跨域 hreflang
- [ ] `web/src/app/sitemap.ts` — 确认 ja 归属切换后仍正确
- [ ] `web/tests/lib/brands.test.ts` — `ownerBrand()` 单测
- [ ] `web/tests/middleware.test.ts` — 双向 301 单测 10+ 个 case
- [ ] `web/tests/e2e/cross-domain-redirect.spec.ts` — Playwright
- [ ] `yarn build` + `yarn test` + `yarn test:e2e` 绿
- [ ] Amplify preview 冒烟
- [ ] 生产部署后 `curl -I -L` 验证 301 chain

### PR-3 (Sweep)

- [ ] grep `web/messages/**/*.json` 里剩余 `/\bKaitu\b/`（zh）+ `/kaitu/i`（en/ja）
- [ ] grep `web/content/**/*.md`（排除 `/k2`）
- [ ] grep `web/src/**/*.{tsx,ts}` 硬编码字符串
- [ ] 术语表应用（审查 / GFW / 翻墙等）
- [ ] `messages-integrity.test.ts` 扫描范围扩到全部 namespace
- [ ] 手测 `/install` / `/support` / `/purchase` 三 locale
- [ ] `yarn build` + `yarn test` 绿
