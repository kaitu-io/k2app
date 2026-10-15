/**
 * Content Posts Helper
 *
 * Velite-backed access to non-k2 content pages served by the
 * `[locale]/[...slug]` catch-all: `/{category}` lists a category,
 * `/{category}/{post}` renders a post. Velite slugs follow
 * `{category}/{post}` (from `content/{locale}/{category}/{post}.md`).
 *
 * Locale fallback mirrors `findK2Post` in `lib/k2-posts.ts`: an exact locale
 * match wins, otherwise the brand's default locale fills in.
 */
import { posts } from '#velite';
import { siteBrand, type Brand, type BrandId } from './brands';
import { isPostVisibleToBrand, type K2Post } from './k2-posts';
import { siteConfig, fillBrandTemplate } from './site';

/** A Velite content post (K2Post shape plus the cover image frontmatter). */
export type ContentPost = K2Post & { coverImage?: string };

/**
 * Category registry lives in `lib/site/kaitu.ts` (contentCategories) — the only
 * categories the catch-all serves. A slug whose first segment is not listed
 * there 404s even if a markdown file exists, so adding a new content
 * directory means registering it in the site config (and checking the
 * reserved-paths list in web/CLAUDE.md — static routes win over the catch-all).
 */
export interface ContentCategory {
  slug: string;
  name: string;
  description?: string;
  /** 列表页分组（有序，已按 locale 取名）；未配置时为空数组。 */
  sections: { key: string; name: string }[];
}

/** 列表页的一组文章。key 为 null 的是末尾的「更多」组。 */
export interface ContentPostGroup {
  key: string | null;
  name: string;
  posts: ContentPost[];
}

/** Resolve a registered category for the given locale, or null. */
export function findCategory(locale: string, slug: string, brand: Brand = siteBrand()): ContentCategory | null {
  const def = siteConfig().contentCategories[slug];
  if (!def) return null;
  const pick = (m: Partial<Record<string, string>> | undefined) => m?.[locale] ?? m?.[brand.defaultLocale];
  const description = pick(def.description);
  return {
    slug,
    name: pick(def.name) ?? slug,
    description: description === undefined ? undefined : fillBrandTemplate(description, brand),
    sections: (def.sections ?? []).map((sec) => ({ key: sec.key, name: fillBrandTemplate(pick(sec.name) ?? sec.key, brand) })),
  };
}

/**
 * 按分类配置的 sections 给列表页分组：组顺序 = 配置顺序；组内带 frontmatter `order` 的置顶（升序，
 * 用于专题的汇总页），其余保持传入顺序（新到旧）；
 * 没有 section 或 section 未登记的文章进末尾的「更多」组；空组不返回。
 * 分类没配 sections 时返回单组（key null、name 为空），调用方据此不渲染组标题。
 */
export function groupPostsBySection(
  posts: ContentPost[],
  sections: ContentCategory['sections'],
  otherName: string
): ContentPostGroup[] {
  if (sections.length === 0) return posts.length ? [{ key: null, name: '', posts }] : [];
  const known = new Set(sections.map((sec) => sec.key));
  // Array.prototype.sort 是稳定的：没有 order 的文章保持传入的新到旧顺序。
  const pinnedFirst = (list: ContentPost[]) =>
    [...list].sort((a, b) => (a.order ?? Infinity) - (b.order ?? Infinity));
  const groups: ContentPostGroup[] = sections.map((sec) => ({
    key: sec.key,
    name: sec.name,
    posts: pinnedFirst(posts.filter((p) => p.section === sec.key)),
  }));
  groups.push({
    key: null,
    name: otherName,
    posts: posts.filter((p) => !p.section || !known.has(p.section)),
  });
  return groups.filter((g) => g.posts.length > 0);
}

/** Registered category slugs (for static params generation). */
export function categorySlugs(): string[] {
  return Object.keys(siteConfig().contentCategories);
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

/** Locales with their own (non-fallback) version of `{category}/{postSlug}`. */
export function contentPostLocales(
  category: string,
  postSlug: string,
  brand: Brand = siteBrand()
): string[] {
  const slug = `${category}/${postSlug}`;
  return [
    ...new Set(
      (posts as ContentPost[])
        .filter((p) => p.slug === slug && isServablePost(p, category, brand.id))
        .map((p) => p.locale)
    ),
  ];
}

/**
 * Locales with at least one post of their own in the category. A locale outside
 * this set only lists default-locale fallbacks — a duplicate of that listing.
 */
export function categoryLocales(category: string, brand: Brand = siteBrand()): string[] {
  return [
    ...new Set(
      (posts as ContentPost[])
        .filter((p) => isServablePost(p, category, brand.id))
        .map((p) => p.locale)
    ),
  ];
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
