import { MetadataRoute } from 'next';
import { routing } from '@/i18n/routing';
import { posts } from '#velite';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

export default function sitemap(): MetadataRoute.Sitemap {
  // Static pages in the application
  const staticPages = [
    '',           // Home page
    '/login',
    '/discovery',
    '/install',
    '/opensource',
    '/privacy',
    '/purchase',
    '/routers',
    '/terms',
  ];

  const sitemapEntries: MetadataRoute.Sitemap = [];

  // Generate entries for static pages
  staticPages.forEach(page => {
    routing.locales.forEach(locale => {
      const url = `${baseUrl}/${locale}${page}`;

      const alternates: Record<string, string> = {};
      routing.locales.forEach(altLocale => {
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
      languages: routing.locales.reduce((acc, locale) => {
        acc[locale] = `${baseUrl}/${locale}`;
        return acc;
      }, {} as Record<string, string>),
    },
  });

  // Add content pages from velite (published posts only)
  const publishedPosts = posts.filter((post) => !post.draft);
  for (const post of publishedPosts) {
    sitemapEntries.push({
      url: `${baseUrl}/${post.locale}/${post.slug}`,
      lastModified: new Date(post.date),
      changeFrequency: 'weekly',
      priority: post.slug.startsWith('k2/') ? 0.9 : 0.6,
    });
  }

  return sitemapEntries;
}
