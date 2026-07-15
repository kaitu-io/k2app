/**
 * K2 Posts Helper
 *
 * Filters Velite posts for the k2/ prefix, groups by section, and sorts by order.
 * Used by K2Sidebar, K2Page, and (later) the sitemap generator (T7).
 */
import { posts } from '#velite';
import { siteBrand, type BrandId } from './brands';

/** Frontmatter brand visibility (velite.config.ts: enum, default 'both'). */
export type PostBrand = BrandId | 'both';

/** Shape of a Velite post (with optional k2-specific fields). */
export interface K2Post {
  title: string;
  date: string;
  summary?: string;
  tags?: string[];
  draft: boolean;
  content: string;
  metadata: { readingTime: number; wordCount: number };
  filePath: string;
  locale: string;
  slug: string;
  order?: number;
  section?: string;
  brand?: PostBrand;
}

/**
 * Whether a post may be served by the given brand's deployment.
 *
 * A missing `brand` means 'both' — that is the Velite schema default, and test
 * fixtures pre-dating the field rely on it. Frontmatter `brand: kaitu` is a real
 * gate, not just a sitemap hint: an off-brand post must 404, because a sitemap
 * exclusion neither de-indexes a reachable page nor stops the sidebar and the
 * inter-document links from walking straight into it.
 */
export function isPostVisibleToBrand(
  post: { brand?: PostBrand },
  brandId: BrandId = siteBrand().id
): boolean {
  return !post.brand || post.brand === 'both' || post.brand === brandId;
}

/** A group of k2 posts sharing the same section label. */
export interface K2PostGroup {
  /** Section identifier (e.g. "getting-started", "technical", "comparison"). */
  section: string;
  /** Posts within this section, sorted ascending by `order` (undefined last). */
  posts: K2Post[];
}

/**
 * Return all published k2/ posts for the given locale that this deployment's
 * brand may serve, grouped by section and sorted by `order` within each group.
 *
 * Posts without a `section` field are placed into a fallback group keyed
 * `"uncategorized"`. Posts without an `order` field sort to the end of their
 * section.
 *
 * The brand filter is what keeps the sidebar honest: without it the overleap
 * sidebar would list — and link to — the kaitu-only install docs on every k2 page.
 *
 * @param locale - BCP-47 locale code, e.g. `"zh-CN"`.
 * @param brandId - Serving brand; defaults to the baked deployment brand.
 * @returns Array of `K2PostGroup` objects, ordered by the first appearance of
 *   each section among the sorted posts.
 */
export function getK2Posts(locale: string, brandId: BrandId = siteBrand().id): K2PostGroup[] {
  const k2Posts = (posts as K2Post[]).filter(
    (post) =>
      post.locale === locale &&
      (post.slug === 'k2' || post.slug.startsWith('k2/')) &&
      !post.draft &&
      isPostVisibleToBrand(post, brandId)
  );

  // Sort all k2 posts by order ascending (undefined → Infinity, sorts last)
  const sorted = [...k2Posts].sort(
    (a, b) => (a.order ?? Infinity) - (b.order ?? Infinity)
  );

  // Group by section preserving insertion order
  const groupMap = new Map<string, K2Post[]>();

  for (const post of sorted) {
    const key = post.section ?? 'uncategorized';
    const existing = groupMap.get(key);
    if (existing) {
      existing.push(post);
    } else {
      groupMap.set(key, [post]);
    }
  }

  return Array.from(groupMap.entries()).map(([section, groupPosts]) => ({
    section,
    posts: groupPosts,
  }));
}
