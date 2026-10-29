# kaitu.io SEO guidelines (moved from `web/CLAUDE.md`)

These apply to every public `[locale]` page in `web/`. Rules that a test enforces, or that break
silently, stay in `web/CLAUDE.md`. GEO (AI search) content rules are in
[`docs/marketing/geo-content-rules.md`](../marketing/geo-content-rules.md).

## Technical SEO

- Every public page exports `generateMetadata()` returning title, description, canonical URL and Open Graph tags. No page ships without metadata.
- Structured data (JSON-LD) on public pages: `Organization` (footer/layout), `SoftwareApplication` (install), `FAQPage` (support/guides), `BreadcrumbList` (content pages).
- `sitemap.ts` includes every locale variant with `hreflang` alternates. Add new public routes to the sitemap (static routes are registered in `src/lib/site/kaitu.ts`).
- Images use `next/image` with descriptive `alt` text, not raw `<img>`.
- Strict heading hierarchy: one `<h1>` per page, `<h2>` > `<h3>` nested logically, no skipped levels.
- Meta descriptions: 120-160 characters, primary keyword included naturally, no keyword stuffing.
- URLs are short semantic English words (`/install`, `/purchase`, `/support`), with no version suffixes or IDs.
- Every public page is reachable within 3 clicks from the homepage.

## Content SEO

- Page titles follow `{Page Topic} | 开途` (the registry wordmark, via the SEO templates), at most 60 characters.
- Every Velite content page has `title` and `description` frontmatter; the description is used for meta.
