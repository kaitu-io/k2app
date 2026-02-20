import { MetadataRoute } from 'next';
import { routing } from '@/i18n/routing';
import { getPayload } from 'payload';
import config from '@payload-config';

export const dynamic = 'force-dynamic';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

// Fetch published articles from CMS
async function getPublishedArticles() {
  try {
    const payload = await getPayload({ config });

    // Use Payload's built-in _status field from versions.drafts
    const articles = await payload.find({
      collection: 'articles',
      where: {
        _status: { equals: 'published' },
      },
      limit: 1000,
    });

    return articles.docs;
  } catch (error) {
    console.error('Error fetching articles for sitemap:', error);
    return [];
  }
}

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
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

  // Fetch and add CMS articles
  const articles = await getPublishedArticles();

  articles.forEach(article => {
    if (!article.path) return;

    routing.locales.forEach(locale => {
      const url = `${baseUrl}/${locale}${article.path}`;

      const alternates: Record<string, string> = {};
      routing.locales.forEach(altLocale => {
        alternates[altLocale] = `${baseUrl}/${altLocale}${article.path}`;
      });

      sitemapEntries.push({
        url,
        lastModified: article.updatedAt ? new Date(article.updatedAt) : new Date(),
        changeFrequency: 'weekly',
        priority: 0.7,
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

  return sitemapEntries;
}
