/**
 * k2 protocol docs (Velite, content/<locale>/k2/*.md).
 * en-GB is the master; other locales carry a file only where translated and
 * otherwise fall back to en-GB.
 */
import { posts } from '#velite';
import { DEFAULT_LOCALE } from './site';

export interface K2Post {
  title: string;
  date: string;
  summary?: string;
  tags?: string[];
  draft: boolean;
  content: string;
  metadata: { readingTime: number; wordCount: number };
  locale: string;
  slug: string;
  order?: number;
  section?: string;
}

export interface K2PostGroup {
  section: string;
  posts: K2Post[];
}

const all = () => (posts as K2Post[]).filter((p) => !p.draft && (p.slug === 'k2' || p.slug.startsWith('k2/')));

/** Slugs of every published doc (any locale) — the set of valid /k2 routes. */
export function k2Slugs(): string[] {
  return [...new Set(all().map((p) => p.slug))];
}

export function findK2Post(locale: string, slug: string): K2Post | undefined {
  const candidates = all().filter((p) => p.slug === slug);
  return candidates.find((p) => p.locale === locale) ?? candidates.find((p) => p.locale === DEFAULT_LOCALE);
}

/** Sidebar: every doc resolved for `locale` (with fallback), grouped by section, sorted by `order`. */
export function getK2Groups(locale: string): K2PostGroup[] {
  const resolved = k2Slugs()
    .map((slug) => findK2Post(locale, slug))
    .filter((p): p is K2Post => !!p)
    .sort((a, b) => (a.order ?? Infinity) - (b.order ?? Infinity));
  const groups = new Map<string, K2Post[]>();
  for (const p of resolved) {
    const key = p.section ?? 'uncategorized';
    groups.set(key, [...(groups.get(key) ?? []), p]);
  }
  return [...groups.entries()].map(([section, list]) => ({ section, posts: list }));
}
