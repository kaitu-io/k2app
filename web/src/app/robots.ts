import { MetadataRoute } from 'next';
import { siteBrand } from '@/lib/brands';

// The brand is baked at build time — robots.txt is fully static per deployment.
export default function robots(): MetadataRoute.Robots {
  const brand = siteBrand();
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
