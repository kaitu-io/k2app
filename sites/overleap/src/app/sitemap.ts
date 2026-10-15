import type { MetadataRoute } from 'next';
import { listGuides } from '@/lib/guides';
import { hreflangLanguages } from '@/lib/metadata';
import { DEFAULT_LOCALE, INDEXED_LOCALES, SITE, STATIC_ROUTES } from '@/lib/site';

// Only indexed (human-reviewed) locales; the machine-translated ones are noindex.
export default function sitemap(): MetadataRoute.Sitemap {
  const pages = STATIC_ROUTES.flatMap((path) =>
    INDEXED_LOCALES.map((locale) => ({
      url: `${SITE.baseUrl}/${locale}${path}`,
      changeFrequency: 'weekly' as const,
      priority: path === '' ? 1 : locale === DEFAULT_LOCALE ? 0.8 : 0.6,
      alternates: { languages: hreflangLanguages(path) },
    })),
  );
  // Guides: drafts are already filtered out of a production build by listGuides().
  const guides = listGuides().flatMap((g) =>
    INDEXED_LOCALES.map((locale) => ({
      url: `${SITE.baseUrl}/${locale}/guides/${g.slug}`,
      lastModified: g.updated,
      changeFrequency: 'monthly' as const,
      priority: locale === DEFAULT_LOCALE ? 0.7 : 0.5,
      alternates: { languages: hreflangLanguages(`/guides/${g.slug}`) },
    })),
  );
  return [...pages, ...guides];
}
