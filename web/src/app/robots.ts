import { MetadataRoute } from 'next';
import { getBrand } from '@/lib/brand-server';

// Must be dynamic so robots.txt content reflects the request host (kaitu.io vs overleap.io).
export const dynamic = 'force-dynamic';

export default async function robots(): Promise<MetadataRoute.Robots> {
  const brand = await getBrand();
  return {
    rules: {
      userAgent: '*',
      allow: '/',
      disallow: ['/api/', '/_next/', '/manager/'],
    },
    sitemap: `${brand.baseUrl}/sitemap.xml`,
    host: brand.baseUrl.replace(/^https?:\/\//, ''),
  };
}
