import { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { siteBrand, type Brand } from '@/lib/brands';
import { siteConfig, fillBrandTemplate } from '@/lib/site';

interface MetadataOverrides {
  title?: string;
  description?: string;
  ogType?: 'website' | 'article';
  ogImage?: string;
  article?: {
    publishedTime?: string;
    modifiedTime?: string;
    section?: string;
    tags?: string[];
  };
}

export function generateMetadata(
  locale: string,
  pathname: string = '',
  overrides: MetadataOverrides = {},
  // Fail-safe, not fail-kaitu: an omitted brand must resolve to the brand this
  // deployment was BUILT for, never to a hardcoded one. The previous `= KAITU`
  // default made every non-passing call site (e.g. /support) publish
  // canonical/hreflang/og:url pointing at the kaitu host from an overleap build.
  brand: Brand = siteBrand()
): Metadata {
  const resolvedBaseUrl = process.env.NEXT_PUBLIC_BASE_URL || brand.baseUrl;

  // 默认 title / description 来自品牌站点结构（lib/site/<brand>.ts seo），locale 缺失时
  // 回落品牌默认语言——绝不回落到另一品牌的语言。
  const seo = siteConfig(brand).seo;
  const pickSeo = (m: Partial<Record<string, string>>) =>
    fillBrandTemplate(m[locale] ?? m[brand.defaultLocale] ?? Object.values(m)[0] ?? '', brand);
  const title = overrides.title || pickSeo(seo.defaultTitle);
  const description = overrides.description || pickSeo(seo.defaultDescription);
  // overrides.ogImage may be an absolute CDN URL (a post coverImage from
  // the CMS media CDN) or a brand-relative path. Only prepend the base URL for
  // the relative case, otherwise we'd concatenate two absolute URLs.
  const ogImageSrc = overrides.ogImage || brand.ogImagePath;
  const ogImageUrl = /^https?:\/\//.test(ogImageSrc)
    ? ogImageSrc
    : `${resolvedBaseUrl}${ogImageSrc}`;
  const ogType = overrides.ogType || 'website';

  // Phase 2: the two brands are fully isolated — hreflang links only this
  // brand's own locales on its own host. x-default is the brand's default
  // locale. No cross-domain linking, ever (spec: 两站互不感知).
  //
  // Hreflang must use the brand's own baseUrl — NOT NEXT_PUBLIC_BASE_URL —
  // because a preview env override would poison the published SEO graph.
  const languages: Record<string, string> = {};
  brand.allowedLocales.forEach(loc => {
    languages[loc.toLowerCase()] = `${brand.baseUrl}/${loc}${pathname}`;
  });
  languages['x-default'] = `${brand.baseUrl}/${brand.defaultLocale}${pathname}`;

  const ogBase = {
    title,
    description,
    url: `${resolvedBaseUrl}/${locale}${pathname}`,
    siteName: brand.displayName,
    locale: locale.replace('-', '_'),
    images: [{ url: ogImageUrl, width: 1200, height: 630, alt: typeof title === 'string' ? title : `${brand.displayName} k2cc` }],
  };

  const openGraph: Metadata['openGraph'] = ogType === 'article' && overrides.article
    ? {
        ...ogBase,
        type: 'article' as const,
        publishedTime: overrides.article.publishedTime,
        modifiedTime: overrides.article.modifiedTime,
        section: overrides.article.section,
        tags: overrides.article.tags,
      }
    : { ...ogBase, type: 'website' as const };

  return {
    title,
    description,
    openGraph,
    twitter: {
      card: 'summary_large_image',
      title,
      description,
      images: [ogImageUrl],
    },
    alternates: {
      canonical: `${resolvedBaseUrl}/${locale}${pathname}`,
      languages,
    },
    // The default brand's faviconPrefix is '' — its URLs stay byte-identical to
    // the legacy root paths (no cache churn). Other brands get a namespaced set.
    icons: {
      icon: [
        { url: `${brand.faviconPrefix}/favicon-16x16.png`, sizes: '16x16', type: 'image/png' },
        { url: `${brand.faviconPrefix}/favicon-32x32.png`, sizes: '32x32', type: 'image/png' },
        { url: `${brand.faviconPrefix}/icon-48x48.png`, sizes: '48x48', type: 'image/png' },
        { url: `${brand.faviconPrefix}/icon-96x96.png`, sizes: '96x96', type: 'image/png' },
        { url: `${brand.faviconPrefix}/icon-192x192.png`, sizes: '192x192', type: 'image/png' },
        { url: `${brand.faviconPrefix}/icon-512x512.png`, sizes: '512x512', type: 'image/png' },
      ],
      shortcut: brand.faviconPrefix ? `${brand.faviconPrefix}/favicon-32x32.png` : '/favicon.ico',
      apple: [
        { url: `${brand.faviconPrefix}/icon-192x192.png`, sizes: '192x192', type: 'image/png' },
        { url: `${brand.faviconPrefix}/icon-512x512.png`, sizes: '512x512', type: 'image/png' },
      ],
    },
  };
}

// Used by routing.locales consumers that want the full locale list regardless of brand.
export const allLocales = routing.locales;
