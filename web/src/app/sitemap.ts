import { MetadataRoute } from 'next';
import { posts } from '#velite';
import { getPayload } from 'payload';
import config from '@payload-config';
import { getBrand } from '@/lib/brand-server';
import { isPostVisibleToBrand } from '@/lib/k2-posts';

// Render at request time — avoids a build-time DB dependency and keeps Payload
// blog listings fresh. (The brand itself is baked at build time via
// NEXT_PUBLIC_BRAND; this is no longer host-aware.)
export const dynamic = 'force-dynamic';

type BlogEntry = { slug: string; updatedAt?: string };

async function fetchBlogPosts(brandId: 'kaitu' | 'overleap'): Promise<BlogEntry[]> {
  const visibilityField = brandId === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap';
  try {
    const payload = await getPayload({ config });
    const { docs } = await payload.find({
      collection: 'posts',
      locale: 'zh-CN',
      where: {
        and: [
          { status: { equals: 'published' } },
          { [visibilityField]: { equals: true } },
        ],
      },
      limit: 500,
      depth: 0,
      overrideAccess: true,
    });
    return (docs as unknown as Array<{ slug: string; updatedAt?: string }>).map((d) => ({
      slug: d.slug,
      updatedAt: d.updatedAt,
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

  // Static pages in the application. Feature-gated surfaces must not be
  // advertised by a brand that 404s them (see routers/page.tsx).
  const staticPages = [
    '',           // Home page
    '/blog',
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
  const publishedPosts = posts.filter(
    (post) =>
      !post.draft &&
      isPostVisibleToBrand(post, brand.id) &&
      (locales as readonly string[]).includes(post.locale)
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
  // Payload posts are filtered server-side by showOnKaitu/showOnOverleap.
  const blogPosts = await fetchBlogPosts(brand.id);
  for (const { slug, updatedAt } of blogPosts) {
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
