import { MetadataRoute } from 'next';
import { posts } from '#velite';
import { getPayload } from 'payload';
import config from '@payload-config';
import { getBrand } from '@/lib/brand-server';

// Render at request time — avoids build-time DB dependency and keeps blog listings fresh.
// Also required for host-aware sitemap (reads `host` header via getBrand()).
export const dynamic = 'force-dynamic';

type BlogEntry = { slug: string; updatedAt?: string; brand?: string | null };

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
    return (docs as unknown as Array<{ slug: string; updatedAt?: string; brand?: string | null }>).map((d) => ({
      slug: d.slug,
      updatedAt: d.updatedAt,
      brand: d.brand,
    }));
  } catch (err) {
    console.error('sitemap: failed to fetch Payload blog posts', err);
    return [];
  }
}

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const brand = await getBrand();
  const baseUrl = brand.baseUrl;
  const locales = brand.allowedLocales;

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
  // Filter by brand visibility. Missing brand is treated as 'both' (Velite schema
  // default, and keeps legacy test fixtures that pre-date the field working).
  const publishedPosts = posts.filter(
    (post) => !post.draft && (!post.brand || post.brand === 'both' || post.brand === brand.id)
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

  // Payload CMS blog posts — all locales share the same slug.
  // DB fetch is tolerant: if unreachable at build time, blog section is simply omitted.
  // Payload schema doesn't yet carry brand (Phase 2), so for now treat all payload
  // posts as brand='both' and emit them under every host.
  const blogPosts = await fetchBlogPosts();
  for (const { slug, updatedAt, brand: postBrand } of blogPosts) {
    // Respect brand field if Payload collection adds it later. Missing/null = visible everywhere.
    if (postBrand && postBrand !== 'both' && postBrand !== brand.id) continue;

    const alternates: Record<string, string> = {};
    locales.forEach(locale => {
      alternates[locale] = `${baseUrl}/${locale}/blog/${slug}`;
    });
    locales.forEach(locale => {
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
