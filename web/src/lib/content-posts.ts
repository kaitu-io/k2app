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

/** A Velite content post (K2Post shape plus the cover image frontmatter). */
export type ContentPost = K2Post & { coverImage?: string };

interface CategoryDef {
  /** Localized display name; brand default locale is the fallback. */
  name: Record<string, string>;
  /** Localized description used for the list page's meta description. */
  description?: Record<string, string>;
}

/**
 * Category registry — the only categories the catch-all serves. A slug whose
 * first segment is not listed here 404s even if a markdown file exists, so
 * adding a new content directory requires registering it (and checking the
 * reserved-paths list in web/CLAUDE.md — static routes win over the catch-all).
 */
const CATEGORIES: Record<string, CategoryDef> = {
  guides: {
    name: {
      'zh-CN': '使用指南',
      'zh-TW': '使用指南',
      'zh-HK': '使用指南',
      'en-US': 'Guides',
      'en-GB': 'Guides',
      'en-AU': 'Guides',
      ja: 'ガイド',
    },
    description: {
      'zh-CN': '使用方法、最佳实践和故障排查指南',
      'zh-TW': '使用方法、最佳實踐與故障排查指南',
      'zh-HK': '使用方法、最佳實踐與故障排查指南',
      'en-US': 'How-to guides, best practices and troubleshooting',
      'en-GB': 'How-to guides, best practices and troubleshooting',
      'en-AU': 'How-to guides, best practices and troubleshooting',
      ja: '使い方ガイド・ベストプラクティス・トラブルシューティング',
    },
  },
};

export interface ContentCategory {
  slug: string;
  name: string;
  description?: string;
}

/** Resolve a registered category for the given locale, or null. */
export function findCategory(locale: string, slug: string, brand: Brand = siteBrand()): ContentCategory | null {
  const def = CATEGORIES[slug];
  if (!def) return null;
  return {
    slug,
    name: def.name[locale] ?? def.name[brand.defaultLocale] ?? slug,
    description: def.description?.[locale] ?? def.description?.[brand.defaultLocale],
  };
}

/** All registered category slugs (for static params generation). */
export function categorySlugs(): string[] {
  return Object.keys(CATEGORIES);
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
