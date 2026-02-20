import { MetadataRoute } from 'next';

export const dynamic = 'force-static';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: '*',
      allow: '/',
      disallow: ['/api/', '/_next/', '/manager/'],
    },
    sitemap: `${baseUrl}/sitemap.xml`,
  };
}