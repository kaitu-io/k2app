# Header / Footer Redesign + Blog Pages Nav

**Date:** 2026-04-23  
**Status:** Approved

## Problem

The current website header is developer-focused ("narcissistic"): it exposes K2 protocol docs, self-deploy quickstart, router config, and GitHub — technical content that regular VPN users don't need or understand. The header should answer user questions, not showcase product internals.

Additionally:
- `KAITU.wordmark` shows `'Kaitu.io'` instead of the correct Chinese brand name `'开途'`
- Blog pages (`/blog`, `/blog/[slug]`) have no `<Header />` or `<Footer />` — users land on a bare page with no navigation

## Scope

4 changes, all within `web/`:

1. **`src/lib/brands.ts`** — fix KAITU wordmark
2. **`src/components/Header.tsx`** — user-focused nav redesign
3. **`src/components/Footer.tsx`** — add Developer column
4. **`src/app/[locale]/blog/page.tsx`** and **`src/app/[locale]/blog/[slug]/page.tsx`** — add Header + Footer

Out of scope: megamenu, Payload Globals for dynamic nav, new content pages for feature/why-us sections.

---

## Change 1 — Brand Wordmark Fix

**File:** `src/lib/brands.ts`

```ts
// Before
wordmark: 'Kaitu.io',

// After
wordmark: '开途',
```

`OVERLEAP.wordmark` is already `'Overleap'` — no change needed.  
Both brands continue to use `brand.wordmark` in Header and Footer via `useBrand()`.

---

## Change 2 — Header Redesign

**File:** `src/components/Header.tsx`

### Nav structure (desktop)

```
[Logo + wordmark]   [产品功能▾] [为什么选{brand}▾] [定价] [帮助▾]   [语言▾] [登录] [免费下载]
```

- **Logo + wordmark**: unchanged — `brand.logoPath` + `brand.wordmark`
- **产品功能 ▾**: dropdown with two sections:
  - 使用场景: 突破网络限制 → `/` (homepage), 家庭全设备保护 → `/` (homepage), 移动端 + 桌面端 → `/install`
  - 支持平台: macOS / Windows / iOS / Android / Linux / 路由器 → all link to `/install`
  - Note: if homepage adds section anchor IDs (`#features` etc.) in future, these links can be updated to scroll anchors
- **为什么选 {brand} ▾**: dropdown (label rendered via `t('nav.whyBrand', { brand: brand.wordmark })`):
  - 速度与稳定性 → `/` (homepage)
  - 安全与隐私承诺 → `/` (homepage)
  - 用户评价 → `/` (homepage)
- **定价**: direct link → `/purchase`
- **帮助 ▾**: dropdown:
  - 快速入门 → `/guides`
  - 常见问题 → `/support`
  - 联系我们 → `/support`
- **语言切换**: existing `<LanguageSwitcher />` — unchanged
- **登录**: existing button — unchanged
- **免费下载**: primary CTA — links to `/install`

The nav item label "为什么选 {brand}" uses `brand.wordmark` so it renders "为什么选 开途" on kaitu and "Why Overleap" (via i18n) on overleap.

### Removed from header (moved to Footer)

- k2 协议 (`/k2`)
- 快速自部署 (`/k2/quickstart`)
- 路由器配置 (`/routers`)
- GitHub icon link
- Download button label changed from "下载" to "免费下载"

### Dropdown behavior

- Desktop: hover or click to open, click outside to close
- Mobile: hamburger expands to full-screen menu; each dropdown group is an accordion
- Existing mobile hamburger structure is kept; nav items updated

### i18n

New keys needed in all 7 locale `messages/*/nav.json` files:

```json
{
  "nav": {
    "productFeatures": "产品功能",
    "whyBrand": "为什么选 {brand}",
    "useCases": "使用场景",
    "breakGFW": "突破网络限制",
    "familyProtection": "家庭全设备保护",
    "mobilePlusDesktop": "移动端 + 桌面端",
    "supportedPlatforms": "支持平台",
    "speedStability": "速度与稳定性",
    "securityPrivacy": "安全与隐私承诺",
    "testimonials": "用户评价",
    "pricing": "定价",
    "help": "帮助",
    "quickStart": "快速入门",
    "faq": "常见问题",
    "contactUs": "联系我们",
    "freeDownload": "免费下载"
  }
}
```

English translations needed for en-US / en-GB / en-AU / ja locales.

---

## Change 3 — Footer: Add Developer Column

**File:** `src/components/Footer.tsx`

The `nav.footer.developer` i18n section already exists in `messages/zh-CN/nav.json` with keys: `title`, `apiDocs`, `openSource`, `techBlog`. It is currently not rendered.

Add a fifth column "开发者" between Support and Legal:

```
开发者
├── k2 协议文档  → /k2
├── 快速自部署   → /k2/quickstart
├── 路由器配置   → /routers
├── GitHub 开源  → (external github link)
└── Changelog   → /releases
```

i18n: add `k2Docs`, `selfDeploy`, `routerConfig`, `changelog` keys to `nav.footer.developer` in all 7 locales. Existing `apiDocs`, `openSource`, `techBlog` keys can be repurposed or supplemented.

Footer column order: **brand description → 产品 → 开发者 → 支持 → 法律** (5 columns, `md:grid-cols-5`). Brand description column moves to last on small screens via CSS order.

---

## Change 4 — Blog Pages: Add Header + Footer

**Files:**
- `src/app/[locale]/blog/page.tsx`
- `src/app/[locale]/blog/[slug]/page.tsx`

Both pages currently render bare content with no navigation. Wrap content with `<Header />` and `<Footer />`, following the same pattern as `install/page.tsx`:

```tsx
import Header from '@/components/Header'
import Footer from '@/components/Footer'

return (
  <>
    <Header />
    <main>...</main>
    <Footer />
  </>
)
```

No other changes to blog page logic.

---

## Testing

- Verify `brand.wordmark` renders `'开途'` on `kaitu.io` locale pages and `'Overleap'` on `overleap` locale pages
- Verify all 4 dropdown items open/close correctly on desktop
- Verify mobile hamburger accordion works for all dropdown groups
- Verify Footer renders 5 columns with developer links intact
- Verify `/zh-CN/blog` and `/zh-CN/blog/mcp-smoke-test-2` show Header and Footer
- Verify all new i18n keys are present in all 7 locale files (no raw key passthrough)
- Run `yarn lint` — no ESLint errors (especially `@/i18n/routing` Link import rule)
