# Overleap Brand Differentiation on Shared web/ Deployment

*Date: 2026-04-21 · Owner: david · Status: DRAFT · Target: W2 (2026-04-28) 英文博客首发前 merge*

## Context

`web/` 是单一 Next.js 站点（App Router + next-intl + Velite + Payload），目前通过 AWS Amplify 部署（app `d3q8wll74rs94h` 分支 `website`）。

2026-04-21 品牌架构决策（见 `docs/marketing/brand-naming-strategy.md`）：Overleap 是海外母品牌，Kaitu 是中国产品名。本文档 spec **同一个 Next.js build 同时服务 `kaitu.io`（Kaitu 品牌）和 `overleap.io`（Overleap 品牌）**，不做多 deployment 拆分。

overleap.io DNS 已挂到同一个 Amplify app（2026-04-21 完成），当前访问 `https://overleap.io` 显示的是 Kaitu 中文内容（因为代码无 host 感知）。本 spec 负责让 overleap.io 呈现独立 Overleap 品牌面。

## Goals

- 访问 `overleap.io` 看到 Overleap 品牌（wordmark、logo、footer、metadata、sitemap），默认 en-US，不暴露 zh-* locale。
- 访问 `kaitu.io` 行为不变（保持现状所有 locale + 开途 / Kaitu 品牌）。
- W2 (2026-04-28) 第一篇英文博客 "k2cc vs BBR vs CUBIC under packet loss" 能在 `overleap.io/en-US/blog/k2cc-vs-bbr-benchmark` 发布，也同时在 `kaitu.io/en-US/blog/` 列表出现（协议层内容共享，tag `brand: both` + `canonicalBrand: overleap` 让 SEO 权重归 Overleap）。
- **Brand 叙事型内容**（如 W13 "Overleap's first 100 paying users" 复盘 / "开途告家人书"）tag 单边 brand，只在对应 host 可见。
- 单一代码库、单一 Amplify build、单一部署流水线。

## Non-Goals

- 拆分为两套独立 Next.js deployment（成本高、W2 来不及）。
- overleap.io 的付费 / 登录 / Install / Support 页本次完整打磨 —— 这些对 W2 博客发稿不是阻塞项，做最小兜底即可（用 Coming Soon 或保留，下轮迭代）。
- 多品牌 Payload CMS admin 隔离 —— admin (`/manager/cms`) 继续单一品牌视角（Kaitu 运营同事用），不做 brand 过滤。
- 两套 Google Analytics 属性切换 —— 留作后续工作。
- overleap.io 的 zh-* 内容（overleap 明确只服务海外英文市场）。

## Architecture

### 核心设计：Middleware 解析 host → 请求头注入 brand → RSC / 组件读取

```
Request → middleware.ts
            │
            ├─ 读 req.headers.host → brandFromHost() → Brand config
            ├─ 注入 `x-brand` 请求头（供 downstream RSC 读取）
            ├─ overleap.io 且 pathname 含 zh-* locale → 307 → /en-US/...
            ├─ overleap.io 且 pathname = "/" → 强制 307 → /en-US（不看 Accept-Language）
            └─ kaitu.io → 保留现有 Accept-Language + cookie 行为
                  │
                  ▼
          RSC / Layout / Components
                  │
                  └─ getBrand() 从 headers() 读 x-brand → 返回 Brand config
                      │
                      └─ 渲染时用 brand.displayName / brand.logoPath / brand.baseUrl / ...
```

Next.js 14+ 支持 middleware 在 `NextResponse.next()` 时通过 `request.headers` 注入头，RSC 能通过 `headers()` API 读到。这是纯 SSR 时机、零客户端 JS 成本。

### Brand Config 单一事实源

新文件 `web/src/lib/brands.ts`：

```ts
export type BrandId = 'kaitu' | 'overleap';

export type Brand = {
  id: BrandId;
  displayName: string;           // "Kaitu" / "Overleap"
  wordmark: string;              // "Kaitu.io" / "Overleap"
  legalName: string;             // "Kaitu LLC" / "Overleap" (pending legal entity)
  baseUrl: string;               // "https://kaitu.io" / "https://overleap.io"
  defaultLocale: Locale;         // "zh-CN" / "en-US"
  allowedLocales: Locale[];      // Kaitu: all 7 / Overleap: ['en-US','en-GB','en-AU']
  logoPath: string;              // "/kaitu-icon.png" / "/overleap-icon.png"
  contactEmail: string;          // "support@kaitu.me" / "support@overleap.io"
  taglineZh?: string;            // "愿上帝为你开路" (kaitu only, overleap omits)
  ogImagePath: string;           // per-brand OG image
};

export const KAITU: Brand = { /* ... */ };
export const OVERLEAP: Brand = { /* ... */ };

const HOST_MAP: Record<string, Brand> = {
  'kaitu.io': KAITU,
  'www.kaitu.io': KAITU,
  'overleap.io': OVERLEAP,
  'www.overleap.io': OVERLEAP,
};

export function brandFromHost(host: string | null | undefined): Brand {
  if (!host) return KAITU; // default fallback
  // strip port, lowercase
  const h = host.toLowerCase().split(':')[0];
  return HOST_MAP[h] ?? KAITU;
}
```

### Server-side brand helper

新文件 `web/src/lib/brand-server.ts`（server-only）：

```ts
import { headers } from 'next/headers';
import { brandFromHost, KAITU, type Brand } from './brands';

export async function getBrand(): Promise<Brand> {
  const h = await headers();
  const brandId = h.get('x-brand');
  if (brandId === 'overleap') return OVERLEAP;
  if (brandId === 'kaitu') return KAITU;
  // fallback: read host directly (e.g. when middleware header wasn't set)
  return brandFromHost(h.get('host'));
}
```

## File-by-file Changes

### 1. `web/src/middleware.ts` — 注入 brand + 约束 locale

在现有 pathname 分发逻辑前加 brand 解析：

```ts
import { brandFromHost } from '@/lib/brands';

export function middleware(req: NextRequest) {
  const host = req.headers.get('host');
  const brand = brandFromHost(host);

  // 克隆 request headers 加 x-brand
  const reqHeaders = new Headers(req.headers);
  reqHeaders.set('x-brand', brand.id);

  // 已有 /i/k2、/admin、/manager、/payload 透传逻辑保持

  const pathname = req.nextUrl.pathname;

  // Overleap: 访问 zh-* locale 的路径 → 重定向到 en-US
  if (brand.id === 'overleap') {
    const localeMatch = pathname.match(/^\/(zh-CN|zh-TW|zh-HK|ja)(\/.*)?$/);
    if (localeMatch) {
      const rest = localeMatch[2] ?? '';
      return NextResponse.redirect(new URL(`/en-US${rest}`, req.url), 307);
    }
  }

  // 根路径处理
  if (pathname === '/') {
    const targetLocale = brand.id === 'overleap'
      ? 'en-US'
      : (readCookie('preferredLocale') ?? getBestLocale(req.headers.get('accept-language'), brand.allowedLocales));
    const res = NextResponse.redirect(new URL(`/${targetLocale}`, req.url), 307);
    // 仅 Kaitu 在首次访问时 set suggestedLocale cookie；Overleap 不 set（强制 en-US）
    if (brand.id === 'kaitu' && !req.cookies.get('hasVisited')) {
      res.cookies.set('suggestedLocale', targetLocale, ...);
      res.cookies.set('hasVisited', 'true', ...);
    }
    return res;
  }

  // 其余路径交给 intlMiddleware，但把 reqHeaders 传进去
  return intlMiddleware(req, { headers: reqHeaders });
}
```

`getBestLocale()` 需要改签名，接受 `allowedLocales` 参数，在允许集合内选。

### 2. `web/src/lib/brands.ts` — 新增（见上）

### 3. `web/src/lib/brand-server.ts` — 新增（见上）

### 4. `web/src/components/Header.tsx` — 读 brand 替换硬编码

```tsx
import { getBrand } from '@/lib/brand-server';

export default async function Header() {
  const brand = await getBrand();
  return (
    <header>
      <Image src={brand.logoPath} alt={`${brand.displayName} Logo`} ... />
      <span>{brand.wordmark}</span>
      {/* ... */}
    </header>
  );
}
```

当前 Header 是 Client Component 还是 Server Component? 探索确认为 Server（App Router 默认）。若是 Client，需要把 brand 通过 layout 传入 props。

### 5. `web/src/components/Footer.tsx` — copyright / logo brand-aware

- Line 18 `src="/kaitu-icon.png"` → `src={brand.logoPath}`
- Line 24 `{t('nav.footer.brandName')}` → 保持 i18n（brandName 翻译值已含 Overleap），或改用 `{brand.wordmark}` 更稳
- Line 101 tagline `"愿上帝为你开路"` → `{brand.taglineZh && locale.startsWith('zh') ? brand.taglineZh : null}`（Overleap 的 `taglineZh` 不设，自然 falsy）
- Line 102 `"Kaitu LLC"` → `© {new Date().getFullYear()} {brand.legalName}`

### 6. `web/src/app/[locale]/metadata.ts` — siteName 动态

```ts
export async function generateMetadata({ params }): Promise<Metadata> {
  const brand = await getBrand();
  const locale = (await params).locale;
  // ...
  return {
    title: ...,
    openGraph: { siteName: brand.displayName, url: brand.baseUrl, /* ... */ },
    alternates: { canonical: `${brand.baseUrl}/${locale}` },
  };
}
```

### 7. `web/src/app/[locale]/page.tsx` — line 31 hardcoded suffix

`` `${t('hero.title')} | Kaitu k2cc` `` → `` `${t('hero.title')} | ${brand.wordmark} k2cc` ``

### 8. `web/src/app/sitemap.ts` — host-aware baseUrl + locale filter

```ts
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const brand = await getBrand();
  const locales = brand.allowedLocales;
  const paths = [...]; // existing path enumeration
  return paths.flatMap(path =>
    locales.map(locale => ({
      url: `${brand.baseUrl}/${locale}${path}`,
      // ...
    }))
  );
}
```

### 9. `web/src/app/robots.ts` — host-aware

```ts
const brand = await getBrand();
return {
  rules: [...],
  sitemap: `${brand.baseUrl}/sitemap.xml`,
  host: brand.baseUrl.replace(/^https:\/\//, ''),
};
```

### 10. `web/src/lib/constants.ts` — COMPANY_INFO 按 brand 取

此文件当前是静态常量，被多处 import。两种方案：
- **A** 把 `COMPANY_INFO` 改成函数 `getCompanyInfo(brand: Brand)`，调用点都加 `await getBrand()`
- **B** 保留 Kaitu 作默认 COMPANY_INFO，Overleap 特殊场景单独导出 OVERLEAP_COMPANY_INFO

**推荐 A**。影响面需先 grep，预计 <10 处。

Download 制品名（`Kaitu_${version}_x64.exe` 等）对 W2 博客不阻塞 —— Overleap 下载流程是 Phase 2 工作，本轮保留 Kaitu 命名 + 不在 overleap.io 首页暴露 `/install` 即可。

### 11. `web/velite.config.ts` — 加 `brand` 字段

```ts
posts: defineCollection({
  schema: s.object({
    title: s.string().max(99),
    brand: s.enum(['kaitu', 'overleap', 'both']).default('both'),
    canonicalBrand: s.enum(['kaitu', 'overleap']).optional(),
    // ... existing fields
  }).transform(...)
})
```

- `brand` = **可见性**：哪个 host 的 `/blog` 列表会显示。`'both'` 为默认，覆盖绝大多数技术 / 竞品 / 场景内容。
- `canonicalBrand` = **SEO 归属**：只在 `brand === 'both'` 时有意义，指定 `<link rel="canonical">` 应该指向哪个 host，避免 Google 判重复内容。
  - 英文优先内容（overleap 主场）→ `canonicalBrand: 'overleap'`
  - 中文优先内容（kaitu 主场）→ `canonicalBrand: 'kaitu'`
  - 省略时默认规则：`locale` 以 `en-` 开头 → canonical = overleap；其他 → canonical = kaitu

**Brand 分类实操指南：**

| 内容类型 | 例子 | brand | canonicalBrand |
|---|---|---|---|
| 协议 / benchmark 技术 | vs-bbr, k2cc spec, 竞品协议对比 | `both` | `overleap`（英文原版）|
| 海外隐私 / 审查 wedge | "VPN in Iran/Russia/China" | `both` | `overleap` |
| 场景化生活（小红书/知乎软文）| "去日本旅游前必装的 3 个应用" | `kaitu` | — |
| 中国特定合规 / GFW 应对 | "六四周边 VPN 故障应对" | `kaitu` | — |
| Brand 叙事 / 复盘 | "Overleap's first 100 paying users" | `overleap` | — |
| 家长 / 家庭指南 | "家里老人也能用的 VPN" | `kaitu` | — |
| 版本 release note | "k2 v0.5 发布" | `both`（中英双份）| `locale` 自动 |

`default('both')` 保证现有 content frontmatter 不加字段也能跑。

### 12. `/blog` 列表 → 按 brand 过滤 + 文章页 canonical

列表过滤：

```ts
const brand = await getBrand();
const visiblePosts = allPosts.filter(p =>
  p.brand === 'both' || p.brand === brand.id
);
```

文章页 `<link rel="canonical">` 计算：

```ts
function resolveCanonical(post: Post, currentBrand: Brand): string {
  if (post.brand !== 'both') {
    // 单边 brand：canonical 必然指向该 brand
    return `${brandById(post.brand).baseUrl}/${post.locale}/blog/${post.slug}`;
  }
  // brand === 'both'：查 canonicalBrand，没写就按 locale 默认
  const target = post.canonicalBrand
    ?? (post.locale.startsWith('en-') ? 'overleap' : 'kaitu');
  return `${brandById(target).baseUrl}/${post.locale}/blog/${post.slug}`;
}
```

Payload CMS posts 和 Velite posts 都需要该过滤 + canonical 逻辑。Payload posts 需要在 collection schema 也加 `brand` + `canonicalBrand` field（见 `web/src/payload/collections/Posts.ts`，本次 spec 不展开，留 Phase 2）。

### 13. Assets

- `web/public/overleap-icon.png` — 放一个 placeholder（可以先用 Kaitu icon 配色变化版，或纯文字 "O" icon）。david 后续给正式 logo 再换。
- `web/public/overleap-og.png` — OG 分享图 placeholder。

### 14. i18n messages — footer brandName 覆盖写法

`web/messages/zh-CN/nav.json` 的 `footer.brandName: "开途 Overleap"` 是历史遗留（当时双品牌并列结构）。按新架构应改：
- zh-CN（只用于 kaitu.io）: `"开途"` 或 `"Kaitu"`（brand 解决了品牌名）
- en-US（主要用于 overleap.io）: `"Overleap"`（已对）

最稳：Footer 不走 i18n brandName 键，直接用 `{brand.wordmark}`，i18n 字段保留但不引用。

## Testing Plan

### 单元测试

- `web/tests/lib/brands.test.ts`:
  - `brandFromHost('kaitu.io')` → KAITU
  - `brandFromHost('overleap.io')` → OVERLEAP
  - `brandFromHost('www.overleap.io')` → OVERLEAP
  - `brandFromHost('overleap.io:3000')` → OVERLEAP（去掉 port）
  - `brandFromHost(undefined)` → KAITU（fallback）
  - `brandFromHost('random.com')` → KAITU（fallback）

### Middleware 集成测试

- host=overleap.io + path=/ → 307 /en-US
- host=overleap.io + path=/zh-CN/blog → 307 /en-US/blog
- host=overleap.io + path=/ja/k2 → 307 /en-US/k2
- host=overleap.io + path=/en-US/blog → 200 pass-through
- host=kaitu.io + path=/ → 沿用现有 Accept-Language / cookie 行为
- host=kaitu.io + path=/zh-CN → 200 pass-through

### E2E（Playwright）

- `curl -H 'Host: overleap.io' https://overleap.io/` → `Location: /en-US`，**不**含 `Set-Cookie: suggestedLocale=...`
- `curl https://overleap.io/en-US` → 200 + HTML 含 "Overleap"、**不含** "Kaitu"、**不含** "开途"
- `curl https://kaitu.io/zh-CN` → 200 + HTML 含 "Kaitu" / "开途"、**不含** "Overleap"（页面纯 zh-CN Kaitu 品牌）
- sitemap 测试：`curl https://overleap.io/sitemap.xml` 只含 en-US URLs、baseUrl=overleap.io
- robots 测试：`curl https://overleap.io/robots.txt` sitemap 指向 overleap.io

### Brand 可见性 / Canonical 测试

假设有三篇测试用 Velite fixture：
- `post-a`（brand: both, canonicalBrand: overleap, locale: en-US）
- `post-b`（brand: kaitu only, locale: zh-CN）
- `post-c`（brand: overleap only, locale: en-US）

- `curl https://overleap.io/en-US/blog` → 含 post-a、post-c 链接；**不含** post-b
- `curl https://kaitu.io/en-US/blog` → 含 post-a 链接；**不含** post-b（zh-CN only）、post-c（overleap only）
- `curl https://kaitu.io/zh-CN/blog` → 含 post-b 链接；**不含** post-a（en-US locale）、post-c
- `curl https://overleap.io/en-US/blog/post-a` → canonical link 指向 `https://overleap.io/en-US/blog/post-a`
- `curl https://kaitu.io/en-US/blog/post-a` → canonical link 指向 `https://overleap.io/en-US/blog/post-a`（同 URL，不同 host，canonical 统一指 overleap 避免重复）

### 手测 smoke

部署 staging（Amplify preview branch）后：
1. 打开 `https://overleap.io/` → 应跳到 `/en-US`，Header 显示 "Overleap"，Footer 显示 "© 2026 Overleap" + 无中文 tagline
2. 打开 `https://kaitu.io/` → 应按 browser 语言检测，中文浏览器跳 `/zh-CN`，Header 显示 "Kaitu.io"、Footer "Kaitu LLC" + "愿上帝为你开路"
3. devtools Network 面板看 response cookies：overleap.io 不应 set `suggestedLocale`

## Rollout

### Phase 1（本 spec，W2 发稿前完成）— 2026-04-28 DDL

1. brand config + middleware host-aware
2. Header / Footer / metadata / page.tsx 读 brand
3. sitemap / robots host-aware
4. Velite schema 加 brand 字段（default 'both' 向下兼容）
5. `/blog` 列表按 brand 过滤
6. 测试（单元 + E2E smoke）
7. Merge 到 `website` 分支 → Amplify 自动部署 → 两个 host 生效

### Phase 2（W3-W5）

- `/install`, `/purchase`, `/support` 页面 overleap.io 版本（或 Coming Soon 卡片）
- overleap.io 独立 OG image 设计
- 支付 / 登录 / 订单系统对 Overleap 流量是否复用 Kaitu 后端 —— 另起 spec
- GA4 按 brand 分属性（`first_content_source` 归因需要）

### Phase 3（W6+）

- Overleap 专属 `/k2/*` 技术文档（或共享 kaitu.io 的技术内容 + 品牌 chrome 不同）
- Overleap 首页独立设计（跳出 Kaitu 模板）

## Known Limitations / Future Work

- **Footer tagline `"愿上帝为你开路"` 本轮通过 `brand.taglineZh` 机制隐藏**，不做大改写。未来 Overleap 可能要英文 tagline，走 brand 配置扩展。
- **Amplify 单 deployment** 意味着任何一侧 build 失败都会同时影响两个 host。风险可接受（同一代码库），但 W2 发稿前务必在 staging 完整回归。
- **Server Component 读 headers 会把页面强制动态渲染**（无法完全 static export）。这对 kaitu.io 当前 PPR/SSG 策略的影响需要验证；若有重大 Lighthouse / TTFB 回退，退回到在 layout 注入 brand context provider，由 client 再读。
- **Payload CMS posts** 的 brand 字段需要单独加 migration —— 但 Payload 当前 content 几乎全空，影响面小。
- **Install scripts `/i/k2`** 当前硬编码 kaitu.io URL，Overleap 用户暂时能访问到（overleap.io/i/k2），但下载的还是 Kaitu 制品。W2 发稿不涉及这条路径，保留不改；Phase 2 再梳理。

## Open Questions

1. **`overleap.io/blog` 是否要等 W2 博客才上线 /blog 链接**？建议：Phase 1 先把 `/blog` 列表空状态跑通（过滤后 0 篇也 OK 显示 "Coming soon"），W2 直接发稿。
2. **Overleap 品牌 logo**：placeholder 够 W2 用，正式 logo 何时出？—— 独立讨论，不阻塞 spec。
3. **`support@overleap.io` mailbox**：Zoho 验证通过后建立，Footer / 联系页要有这个邮箱；spec 已按此假设。
4. ~~**canonical URL 策略**~~ — 已在 Velite schema 段解决：frontmatter 加 `canonicalBrand` 字段，`brand: 'both'` 时由该字段指定 SEO 归属；省略时按 locale 默认（en-* → overleap，其他 → kaitu）。

## References

- 品牌架构决策：`docs/marketing/brand-naming-strategy.md`
- 产品 context：`.agents/product-marketing-context.md`
- 内容日历 W2：`docs/marketing/content-calendar-2026-Q2.md`
- Amplify 挂载：overleap.io → Amplify app `d3q8wll74rs94h` branch `website`（kaitu.io 同 app）
- DNS 状态：`overleap.io` A ALIAS → `d3512w6f4mt599.cloudfront.net`（Amplify 管理）

## Implementation Checklist

- [ ] `web/src/lib/brands.ts` 新建 + KAITU / OVERLEAP / brandFromHost
- [ ] `web/src/lib/brand-server.ts` 新建 + getBrand() 读 headers
- [ ] `web/src/middleware.ts` 加 host 读取、x-brand 注入、Overleap 的 locale 约束
- [ ] `web/src/i18n/request.ts` / `routing.ts` 检查是否需要配合 brand.allowedLocales 调整
- [ ] `web/src/components/Header.tsx` 读 brand 替换 logo + wordmark
- [ ] `web/src/components/Footer.tsx` 读 brand 替换 logo + legalName + taglineZh
- [ ] `web/src/app/[locale]/page.tsx` line 31 hardcoded suffix 改 brand-aware
- [ ] `web/src/app/[locale]/metadata.ts` siteName + baseUrl + canonical 按 brand
- [ ] `web/src/app/sitemap.ts` host-aware baseUrl + locale 过滤
- [ ] `web/src/app/robots.ts` host-aware baseUrl
- [ ] `web/src/lib/constants.ts` COMPANY_INFO 改函数化
- [ ] `web/velite.config.ts` posts schema 加 `brand`（default 'both'）+ `canonicalBrand` optional 字段
- [ ] `/blog` 列表过滤 brand + 文章页 `resolveCanonical()` 计算
- [ ] Payload CMS posts collection schema 加 brand 字段（或留到 Phase 2）
- [ ] `web/public/overleap-icon.png` + `overleap-og.png` placeholder
- [ ] 单元测试 `web/tests/lib/brands.test.ts`
- [ ] E2E smoke：overleap.io / kaitu.io 双 host 回归
- [ ] Staging / preview 部署验证
- [ ] Merge 到 `website` → Amplify 部署 → 生产验证
