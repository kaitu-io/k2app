/**
 * Content Posts Helper
 *
 * Velite-backed access to non-k2 content pages served by the
 * `[locale]/[...slug]` catch-all: `/{category}` lists a category,
 * `/{category}/{post}` renders a post. Velite slugs follow
 * `{category}/{post}` (from `content/{locale}/{category}/{post}.md`).
 *
 * Locale fallback mirrors `findK2Post` in `k2/[[...path]]/page.tsx`: an exact
 * locale match wins, otherwise the BRAND's default locale fills in — never a
 * hardcoded 'zh-CN', which is a kaitu-only locale.
 */
import { posts } from '#velite';
import { siteBrand, type Brand, type BrandId } from './brands';
import { isPostVisibleToBrand, type K2Post } from './k2-posts';
import { siteConfig, fillBrandTemplate } from './site';

/** A Velite content post (K2Post shape plus the cover image frontmatter). */
export type ContentPost = K2Post & { coverImage?: string };

/**
 * Category registry lives in `lib/site/<brand>.ts` (contentCategories) — the only
 * categories the catch-all serves for that brand. A slug whose first segment is
 * not listed there 404s even if a markdown file exists, so adding a new content
 * directory means registering it in the brand's site config (and checking the
 * reserved-paths list in web/CLAUDE.md — static routes win over the catch-all).
 */
export interface ContentCategory {
  slug: string;
  name: string;
  description?: string;
}

/** Resolve a registered category for the given locale, or null. */
export function findCategory(locale: string, slug: string, brand: Brand = siteBrand()): ContentCategory | null {
  const def = siteConfig(brand).contentCategories[slug];
  if (!def) return null;
  const pick = (m: Partial<Record<string, string>> | undefined) => m?.[locale] ?? m?.[brand.defaultLocale];
  const description = pick(def.description);
  return {
    slug,
    name: pick(def.name) ?? slug,
    description: description === undefined ? undefined : fillBrandTemplate(description, brand),
  };
}

/** This brand's registered category slugs (for static params generation). */
export function categorySlugs(brand: Brand = siteBrand()): string[] {
  return Object.keys(siteConfig(brand).contentCategories);
}

function isServablePost(post: ContentPost, category: string, brandId: BrandId): boolean {
  return (
    post.slug.startsWith(`${category}/`) &&
    !post.draft &&
    isPostVisibleToBrand(post, brandId)
  );
}

/**
 * Find one post by `{category}/{postSlug}` with brand-default locale fallback.
 */
export function findContentPost(
  locale: string,
  category: string,
  postSlug: string,
  brand: Brand = siteBrand()
): ContentPost | undefined {
  const slug = `${category}/${postSlug}`;
  const candidates = (posts as ContentPost[]).filter(
    (p) => p.slug === slug && isServablePost(p, category, brand.id)
  );

  const exactMatch = candidates.find((p) => p.locale === locale);
  if (exactMatch) return exactMatch;

  if (locale !== brand.defaultLocale) {
    return candidates.find((p) => p.locale === brand.defaultLocale);
  }

  return undefined;
}

/**
 * List a category's posts for the given locale, newest first. A slug missing
 * in this locale falls back to the brand default locale's version, so the
 * list page never hides a post that the detail page would happily serve.
 */
export function listCategoryPosts(
  locale: string,
  category: string,
  brand: Brand = siteBrand()
): ContentPost[] {
  const candidates = (posts as ContentPost[]).filter((p) =>
    isServablePost(p, category, brand.id)
  );

  const bySlug = new Map<string, ContentPost>();
  for (const post of candidates) {
    if (post.locale === locale) {
      // Exact locale always wins, even over an earlier fallback entry.
      bySlug.set(post.slug, post);
    } else if (post.locale === brand.defaultLocale) {
      const existing = bySlug.get(post.slug);
      if (!existing || existing.locale !== locale) bySlug.set(post.slug, post);
    }
  }

  return [...bySlug.values()].sort(
    (a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()
  );
}
