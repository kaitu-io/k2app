import { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { KAITU, ownerBrand, brandById, type Brand } from '@/lib/brands';

// Legacy export retained for pages that stay Kaitu-branded regardless of host
// (e.g., k2 protocol docs, support page — see spec 2026-04-21-overleap-brand-web-design).
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
    'zh-CN': '开途 k2cc — 30% 丢包照样满速的隐身隧道',
    'zh-TW': '開途 k2cc — 30% 丟包照樣滿速的隱身隧道',
    'zh-HK': '開途 k2cc — 30% 丟包照樣滿速的隱身隧道',
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
  const ogImageRelative = overrides.ogImage || brand.ogImagePath;
  const ogImageUrl = `${resolvedBaseUrl}${ogImageRelative}`;
  const ogType = overrides.ogType || 'website';

  // Cross-domain hreflang: each locale points to its owning brand's host,
  // so Googlebot can link kaitu.io (zh-*) and overleap.io (en-*/ja) as a
  // single multi-regional property. Hreflang must use the brand-resolved
  // base URL — NOT NEXT_PUBLIC_BASE_URL — because a preview env override
  // would break cross-domain linking. See spec §"Known Limitations".
  const languages: Record<string, string> = {};
  routing.locales.forEach(loc => {
    const ownerBaseUrl = brandById(ownerBrand(loc)).baseUrl;
    languages[loc.toLowerCase()] = `${ownerBaseUrl}/${loc}${pathname}`;
  });

  // x-default points to the Chinese main market on kaitu.io (spec's choice).
  languages['x-default'] = `${KAITU.baseUrl}/${KAITU.defaultLocale}${pathname}`;

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
    icons: {
      icon: [
        { url: '/favicon-16x16.png', sizes: '16x16', type: 'image/png' },
        { url: '/favicon-32x32.png', sizes: '32x32', type: 'image/png' },
        { url: '/icon-48x48.png', sizes: '48x48', type: 'image/png' },
        { url: '/icon-96x96.png', sizes: '96x96', type: 'image/png' },
        { url: '/icon-192x192.png', sizes: '192x192', type: 'image/png' },
        { url: '/icon-512x512.png', sizes: '512x512', type: 'image/png' },
      ],
      shortcut: '/favicon.ico',
      apple: [
        { url: '/icon-192x192.png', sizes: '192x192', type: 'image/png' },
        { url: '/icon-512x512.png', sizes: '512x512', type: 'image/png' },
      ],
    },
  };
}

// Used by routing.locales consumers that want the full locale list regardless of brand.
export const allLocales = routing.locales;
