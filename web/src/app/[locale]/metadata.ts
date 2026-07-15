import { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { KAITU, type Brand } from '@/lib/brands';

// Legacy export: the default-brand base URL, retained for the few pages that
// still import it directly (k2 protocol docs, support page).
export const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || KAITU.baseUrl;

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
  brand: Brand = KAITU
): Metadata {
  const resolvedBaseUrl = process.env.NEXT_PUBLIC_BASE_URL || brand.baseUrl;

  const titles: Record<string, string> = {
    'zh-CN': `${brand.wordmark} k2cc — 30% 丢包照样满速的隐身隧道`,
    'zh-TW': `${brand.wordmark} k2cc — 30% 丟包照樣滿速的隱身隧道`,
    'zh-HK': `${brand.wordmark} k2cc — 30% 丟包照樣滿速的隱身隧道`,
    'en-US': `${brand.displayName} k2cc — Full Speed Through 30% Packet Loss`,
    'en-GB': `${brand.displayName} k2cc — Full Speed Through 30% Packet Loss`,
    'en-AU': `${brand.displayName} k2cc — Full Speed Through 30% Packet Loss`,
    'ja': `${brand.displayName} k2cc — 30% パケットロスでもフルスピード`
  };

  const descriptions: Record<string, string> = {
    'zh-CN': 'k2cc 重写拥塞控制规则，30% 丢包依然满速。ECH 加密隐身 + QUIC/TCP-WS 双栈传输，一行命令部署，CT 日志零暴露。',
    'zh-TW': 'k2cc 重寫擁塞控制規則，30% 丟包依然滿速。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
    'zh-HK': 'k2cc 重寫擁塞控制規則，30% 丟包依然滿速。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
    'en-US': 'k2cc rewrites congestion control — full speed at 30% packet loss. ECH stealth encryption + QUIC/TCP-WS dual-stack transport, one-command deployment, zero CT log exposure.',
    'en-GB': 'k2cc rewrites congestion control — full speed at 30% packet loss. ECH stealth encryption + QUIC/TCP-WS dual-stack transport, one-command deployment, zero CT log exposure.',
    'en-AU': 'k2cc rewrites congestion control — full speed at 30% packet loss. ECH stealth encryption + QUIC/TCP-WS dual-stack transport, one-command deployment, zero CT log exposure.',
    'ja': 'k2cc が輻輳制御のルールを書き換え、30% パケットロスでもフルスピード。ECH ステルス暗号化 + QUIC/TCP-WS デュアルスタック転送、1コマンドデプロイ、CT ログゼロ露出。'
  };

  const title = overrides.title || titles[locale] || titles['zh-CN'];
  const description = overrides.description || descriptions[locale] || descriptions['zh-CN'];
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
