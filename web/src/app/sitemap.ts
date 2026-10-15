import { MetadataRoute } from 'next';
import { posts } from '#velite';
import { getBrand } from '@/lib/brand-server';
import { isPostVisibleToBrand } from '@/lib/k2-posts';
import { categoryLocales, categorySlugs } from '@/lib/content-posts';
import { siteConfig } from '@/lib/site';

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const brand = await getBrand();
  const baseUrl = brand.baseUrl;
  const locales = brand.allowedLocales;

  // 静态路由来自站点结构（lib/site/kaitu.ts）：只列真实存在的页面。
  const staticPages = siteConfig().staticRoutes;

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
  // Both filters matter: a post marked for another brand (`brand:` frontmatter)
  // is not served here, and a post in a locale this site does not serve would
  // be advertised under a URL that 404s.
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

  // One entry per locale that has its OWN version of the slug. A zh-TW/zh-HK
  // URL that would render the zh-CN text as a fallback is noindex +
  // canonicalised to zh-CN (see localeSeo), so it must not be advertised here.
  for (const slug of uniqueSlugs) {
    const postsForSlug = publishedPosts.filter(p => p.slug === slug);
    const latestDate = postsForSlug.reduce(
      (latest, p) => (new Date(p.date) > latest ? new Date(p.date) : latest),
      new Date(0)
    );
    const ownLocales = locales.filter(locale => postsForSlug.some(p => p.locale === locale));

    const alternates: Record<string, string> = {};
    ownLocales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/${slug}`;
    });

    ownLocales.forEach(locale => {
      sitemapEntries.push({
        url: `${baseUrl}/${locale}/${slug}`,
        lastModified: latestDate,
        changeFrequency: 'weekly',
        priority: slug.startsWith('k2') ? 0.9 : 0.6,
        alternates: { languages: alternates },
      });
    });
  }

  // Category listing pages (e.g. /guides) — only in locales with at least one
  // post of their own; a locale listing nothing but fallbacks duplicates zh-CN.
  for (const category of categorySlugs()) {
    const ownLocales = locales.filter(locale => categoryLocales(category, brand).includes(locale));
    if (ownLocales.length === 0) continue;

    const alternates: Record<string, string> = {};
    ownLocales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/${category}`;
    });
    ownLocales.forEach(locale => {
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
