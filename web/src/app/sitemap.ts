import { MetadataRoute } from 'next';
import { posts } from '#velite';
import { getBrand } from '@/lib/brand-server';
import { isPostVisibleToBrand } from '@/lib/k2-posts';
import { categorySlugs, listCategoryPosts } from '@/lib/content-posts';

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const brand = await getBrand();
  const baseUrl = brand.baseUrl;
  const locales = brand.allowedLocales;

  // Static pages in the application. Feature-gated surfaces must not be
  // advertised by a brand that 404s them (see routers/page.tsx).
  const staticPages = [
    '',           // Home page
    '/login',
    '/discovery',
    '/install',
    '/opensource',
    '/privacy',
    '/purchase',
    ...(brand.features.releaseNotes ? ['/releases'] : []),
    ...(brand.features.routers ? ['/routers'] : []),
    '/support',
    '/terms',
  ];

  const sitemapEntries: MetadataRoute.Sitemap = [];

  // Generate entries for static pages
  staticPages.forEach(page => {
    locales.forEach(locale => {
      const url = `${baseUrl}/${locale}${page}`;

      const alternates: Record<string, string> = {};
      locales.forEach(altLocale => {
        alternates[altLocale] = `${baseUrl}/${altLocale}${page}`;
      });

      sitemapEntries.push({
        url,
        lastModified: new Date(),
        changeFrequency: page === '' ? 'daily' : 'weekly',
        priority: page === '' ? 1 : 0.8,
        alternates: {
          languages: alternates,
        },
      });
    });
  });

  // Add root redirect page
  sitemapEntries.push({
    url: baseUrl,
    lastModified: new Date(),
    changeFrequency: 'daily',
    priority: 1,
    alternates: {
      languages: locales.reduce((acc, locale) => {
        acc[locale] = `${baseUrl}/${locale}`;
        return acc;
      }, {} as Record<string, string>),
    },
  });

  // Add content pages from velite (published posts only).
  //
  // Both filters matter. Brand visibility is obvious. The locale filter is not:
  // slugs are collected across ALL locales below, so a doc that is kaitu-only in
  // en-US but unmarked in zh-CN would still contribute its slug and get emitted
  // under the overleap locales — advertising a URL that 404s.
  // Only slugs a route actually serves: the /k2 section, or a category
  // registered in content-posts.ts. Anything else would be a sitemap entry
  // pointing at a 404 (the catch-all rejects unregistered categories).
  const servedCategories = categorySlugs();
  const publishedPosts = posts.filter(
    (post) =>
      !post.draft &&
      isPostVisibleToBrand(post, brand.id) &&
      (locales as readonly string[]).includes(post.locale) &&
      (post.slug === 'k2' ||
        post.slug.startsWith('k2/') ||
        servedCategories.includes(post.slug.split('/')[0]))
  );
  const uniqueSlugs = [...new Set(publishedPosts.map(p => p.slug))];

  for (const slug of uniqueSlugs) {
    const postsForSlug = publishedPosts.filter(p => p.slug === slug);
    const latestDate = postsForSlug.reduce(
      (latest, p) => (new Date(p.date) > latest ? new Date(p.date) : latest),
      new Date(0)
    );

    const alternates: Record<string, string> = {};
    locales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/${slug}`;
    });

    locales.forEach(locale => {
      sitemapEntries.push({
        url: `${baseUrl}/${locale}/${slug}`,
        lastModified: latestDate,
        changeFrequency: 'weekly',
        priority: slug.startsWith('k2') ? 0.9 : 0.6,
        alternates: { languages: alternates },
      });
    });
  }

  // Category listing pages (e.g. /guides) — only when this brand's deployment
  // actually lists something there; an empty category page is not advertised.
  for (const category of categorySlugs()) {
    const hasPosts = locales.some(
      (locale) => listCategoryPosts(locale, category, brand).length > 0
    );
    if (!hasPosts) continue;

    const alternates: Record<string, string> = {};
    locales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/${category}`;
    });
    locales.forEach(locale => {
      sitemapEntries.push({
        url: `${baseUrl}/${locale}/${category}`,
        lastModified: new Date(),
        changeFrequency: 'weekly',
        priority: 0.7,
        alternates: { languages: alternates },
      });
    });
  }

  return sitemapEntries;
}
