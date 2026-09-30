import type { MetadataRoute } from 'next';
import { k2Slugs } from '@/lib/k2-posts';
import { DEFAULT_LOCALE, LOCALES, SITE, STATIC_ROUTES } from '@/lib/site';

export default function sitemap(): MetadataRoute.Sitemap {
  const paths = [...STATIC_ROUTES, ...k2Slugs().map((s) => `/${s}`)];
  return paths.flatMap((path) =>
    LOCALES.map((locale) => ({
      url: `${SITE.baseUrl}/${locale}${path}`,
      changeFrequency: 'weekly' as const,
      priority: path === '' ? 1 : locale === DEFAULT_LOCALE ? 0.8 : 0.6,
      alternates: {
        languages: Object.fromEntries([
          ...LOCALES.map((l) => [l.toLowerCase(), `${SITE.baseUrl}/${l}${path}`]),
          ['x-default', `${SITE.baseUrl}/${DEFAULT_LOCALE}${path}`],
        ]),
      },
    })),
  );
}
