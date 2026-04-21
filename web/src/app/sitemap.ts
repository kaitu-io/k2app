import { MetadataRoute } from 'next';
import { routing } from '@/i18n/routing';
import { posts } from '#velite';
import { getPayload } from 'payload';
import config from '@payload-config';

// Render at request time — avoids build-time DB dependency and keeps blog listings fresh.
export const dynamic = 'force-dynamic';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

type BlogEntry = { slug: string; updatedAt?: string };

async function fetchBlogPosts(): Promise<BlogEntry[]> {
  try {
    const payload = await getPayload({ config });
    const { docs } = await payload.find({
      collection: 'posts',
      locale: 'zh-CN',
      where: { status: { equals: 'published' } },
      limit: 500,
      depth: 0,
      overrideAccess: true,
    });
    return (docs as Array<{ slug: string; updatedAt?: string }>).map((d) => ({
      slug: d.slug,
      updatedAt: d.updatedAt,
    }));
  } catch (err) {
    console.error('sitemap: failed to fetch Payload blog posts', err);
    return [];
  }
}

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  // Static pages in the application
  const staticPages = [
    '',           // Home page
    '/blog',
    '/login',
    '/discovery',
    '/install',
    '/opensource',
    '/privacy',
    '/purchase',
    '/releases',
    '/routers',
    '/support',
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
  // Deduplicate by slug, then expand to all locales with hreflang alternates.
  // All locale URLs are valid (generateStaticParams + zh-CN fallback).
  const publishedPosts = posts.filter((post) => !post.draft);
  const uniqueSlugs = [...new Set(publishedPosts.map(p => p.slug))];

  for (const slug of uniqueSlugs) {
    const postsForSlug = publishedPosts.filter(p => p.slug === slug);
    const latestDate = postsForSlug.reduce(
      (latest, p) => (new Date(p.date) > latest ? new Date(p.date) : latest),
      new Date(0)
    );

    const alternates: Record<string, string> = {};
    routing.locales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/${slug}`;
    });

    routing.locales.forEach(locale => {
      sitemapEntries.push({
        url: `${baseUrl}/${locale}/${slug}`,
        lastModified: latestDate,
        changeFrequency: 'weekly',
        priority: slug.startsWith('k2') ? 0.9 : 0.6,
        alternates: { languages: alternates },
      });
    });
  }

  // Payload CMS blog posts — all locales share the same slug.
  // DB fetch is tolerant: if unreachable at build time, blog section is simply omitted.
  const blogPosts = await fetchBlogPosts();
  for (const { slug, updatedAt } of blogPosts) {
    const alternates: Record<string, string> = {};
    routing.locales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/blog/${slug}`;
    });
    routing.locales.forEach(locale => {
      sitemapEntries.push({
        url: `${baseUrl}/${locale}/blog/${slug}`,
        lastModified: updatedAt ? new Date(updatedAt) : new Date(),
        changeFrequency: 'weekly',
        priority: 0.7,
        alternates: { languages: alternates },
      });
    });
  }

  return sitemapEntries;
}
