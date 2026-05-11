/**
 * /k2/[[...path]] — K2 Documentation Page
 *
 * Catches all paths under /k2/. The optional catch-all [[...path]] means:
 *   /k2/          → params.path = undefined  (renders k2/index)
 *   /k2/index     → params.path = ['index']
 *   /k2/architecture → params.path = ['architecture']
 *
 * Velite slugs for k2 content follow the pattern k2/{name} (e.g. k2/index).
 * Content is Velite-processed markdown (trusted build-time source, not user input).
 */
import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { posts } from '#velite';
import { routing } from '@/i18n/routing';
import type { K2Post } from '@/lib/k2-posts';
import { getBrand } from '@/lib/brand-server';
import { generateMetadata as generateBaseMetadata } from '../../metadata';

/** Resolve a slug from the optional catch-all path param. */
function resolveSlug(path: string[] | undefined): string {
  if (!path || path.length === 0) {
    return 'k2';
  }
  return `k2/${path.join('/')}`;
}

/** Build the URL pathname from the catch-all path param. */
function resolvePathname(path: string[] | undefined): string {
  if (!path || path.length === 0) {
    return '/k2';
  }
  return `/k2/${path.join('/')}`;
}

/**
 * Localized Q&A pairs for the /k2/comparison page, surfaced as FAQPage JSON-LD
 * so AI search engines (Google AI Overviews, Perplexity, ChatGPT Search) can
 * extract structured Q&A and cite the page. Mirrors the <h3> comparison sections
 * in content/{locale}/k2/comparison.md.
 *
 * Note: zh-TW / zh-HK currently share COMPARISON_QAS_ZH (Simplified Chinese).
 * This is an acceptable simplification for structured data — the full Traditional
 * Chinese prose lives in the Velite markdown, and AI engines primarily consume
 * JSON-LD for Q&A schema extraction rather than surface rendering. If Traditional
 * Chinese variants become necessary, split into zh-Hant vs zh-Hans maps here.
 */
type ComparisonQA = { question: string; answer: string };

const COMPARISON_QAS_EN: ComparisonQA[] = [
  {
    question: 'How does k2 differ from WireGuard?',
    answer:
      'WireGuard is a plaintext UDP tunnel without TLS disguise; k2 adds ECH + QUIC/TCP-WS dual-stack fallback for stealth and resilience.',
  },
  {
    question: 'How does k2 differ from Shadowsocks?',
    answer:
      'Shadowsocks has only lightweight AEAD without TLS handshake or active-probe defence; k2 adds full TLS 1.3 + ECH handshakes and a reverse proxy on the server.',
  },
  {
    question: 'How does k2 differ from VLESS+Reality?',
    answer:
      'Reality mimics TLS fingerprints but lacks ECH, has no QUIC primary + TCP fallback, and no application-layer congestion control.',
  },
  {
    question: 'How does k2 differ from Hysteria2?',
    answer:
      'Hysteria2 is QUIC-only with no ECH, no TCP fallback, no active-probe defence, and needs a manually configured bandwidth cap.',
  },
];

const COMPARISON_QAS_ZH: ComparisonQA[] = [
  {
    question: 'k2 和 WireGuard 有什么区别？',
    answer:
      'WireGuard 是 UDP 明文隧道，无 TLS 伪装；k2 通过 ECH + QUIC/TCP-WS 双栈降级兼顾隐身与韧性。',
  },
  {
    question: 'k2 和 Shadowsocks 有什么区别？',
    answer:
      'Shadowsocks 只有轻量 AEAD，无 TLS 握手伪装、无主动探测防御；k2 有完整 TLS 1.3 + ECH 握手和服务端反向代理。',
  },
  {
    question: 'k2 和 VLESS+Reality 有什么区别？',
    answer:
      'Reality 有 TLS 指纹模仿但无 ECH、无 QUIC+TCP 双栈、无应用层拥塞控制。',
  },
  {
    question: 'k2 和 Hysteria2 有什么区别？',
    answer:
      'Hysteria2 仅 QUIC，无 ECH、无 TCP 降级、无主动探测对抗，Brutal 需手动设定带宽上限。',
  },
];

const COMPARISON_QAS_JA: ComparisonQA[] = [
  {
    question: 'k2 と WireGuard の違いは？',
    answer:
      'WireGuard は TLS 偽装のない平文 UDP トンネル；k2 は ECH + QUIC/TCP-WS デュアルスタックフォールバックでステルスと耐性を両立。',
  },
  {
    question: 'k2 と Shadowsocks の違いは？',
    answer:
      'Shadowsocks は軽量 AEAD のみで TLS ハンドシェイク偽装もアクティブプローブ防御もない；k2 は完全 TLS 1.3 + ECH ハンドシェイク＋サーバーリバースプロキシ。',
  },
  {
    question: 'k2 と VLESS+Reality の違いは？',
    answer:
      'Reality は TLS 指紋模倣があるが、ECH なし、QUIC+TCP デュアルスタックなし、アプリ層輻輳制御なし。',
  },
  {
    question: 'k2 と Hysteria2 の違いは？',
    answer:
      'Hysteria2 は QUIC のみで、ECH なし、TCP フォールバックなし、アクティブプローブ対策なし、Brutal は帯域を手動設定。',
  },
];

function comparisonQAs(locale: string): ComparisonQA[] {
  if (locale.startsWith('zh')) return COMPARISON_QAS_ZH;
  if (locale === 'ja') return COMPARISON_QAS_JA;
  return COMPARISON_QAS_EN;
}

function buildComparisonFaqPage(locale: string, baseUrlArg: string, pathname: string) {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    '@id': `${baseUrlArg}/${locale}${pathname}#faq`,
    mainEntity: comparisonQAs(locale).map((qa) => ({
      '@type': 'Question',
      name: qa.question,
      acceptedAnswer: { '@type': 'Answer', text: qa.answer },
    })),
  };
}

/** Find a published k2 post by locale + slug, with zh-CN fallback. */
function findK2Post(locale: string, slug: string): K2Post | undefined {
  const exactMatch = (posts as K2Post[]).find(
    (p) => p.locale === locale && p.slug === slug && !p.draft
  );
  if (exactMatch) return exactMatch;

  // Fall back to zh-CN if not found in the requested locale
  if (locale !== 'zh-CN') {
    return (posts as K2Post[]).find(
      (p) => p.locale === 'zh-CN' && p.slug === slug && !p.draft
    );
  }

  return undefined;
}

interface PageParams {
  locale: string;
  path?: string[];
}

export async function generateMetadata({
  params,
}: {
  params: Promise<PageParams>;
}): Promise<Metadata> {
  const { locale, path } = await params;
  const slug = resolveSlug(path);
  const post = findK2Post(locale, slug);
  const pathname = resolvePathname(path);
  const brand = await getBrand();

  if (post) {
    return generateBaseMetadata(
      locale,
      pathname,
      {
        title: post.title,
        description: post.summary,
        ogType: 'article',
        article: {
          publishedTime: post.date,
          modifiedTime: post.date,
          section: post.section,
          tags: post.tags,
        },
      },
      brand
    );
  }

  return generateBaseMetadata(
    locale,
    pathname,
    {
      title: `k2 | ${brand.displayName}`,
    },
    brand
  );
}

export function generateStaticParams(): { locale: string; path: string[] | undefined }[] {
  const params: { locale: string; path: string[] | undefined }[] = [];

  const k2Posts = (posts as K2Post[]).filter(
    (p) => (p.slug === 'k2' || p.slug.startsWith('k2/')) && !p.draft
  );

  for (const post of k2Posts) {
    // Skip the index post (slug "k2") — handled below as path: undefined
    if (post.slug === 'k2') continue;
    // Strip "k2/" prefix to get the path segments
    const pathSegments = post.slug.slice('k2/'.length).split('/');

    params.push({ locale: post.locale, path: pathSegments });

    // Also generate routes for all locales from the routing config
    for (const locale of routing.locales) {
      if (locale !== post.locale) {
        params.push({ locale, path: pathSegments });
      }
    }
  }

  // Add index route (path: undefined) for each locale
  for (const locale of routing.locales) {
    params.push({ locale, path: undefined });
  }

  return params;
}

export const dynamic = 'force-static';

export default async function K2Page({
  params,
}: {
  params: Promise<PageParams>;
}): Promise<React.ReactElement> {
  const { locale, path } = await params;

  setRequestLocale(locale as (typeof routing.locales)[number]);

  const slug = resolveSlug(path);
  const post = findK2Post(locale, slug);

  if (!post) {
    notFound();
  }

  const pathname = resolvePathname(path);
  const brand = await getBrand();

  // Per-article structured data — content is Velite-processed at build time (trusted source)
  const techArticle = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    headline: post.title,
    description: post.summary || '',
    url: `${brand.baseUrl}/${locale}${pathname}`,
    datePublished: post.date,
    dateModified: post.date,
    inLanguage: locale,
    wordCount: post.metadata?.wordCount,
    author: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
    publisher: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
    isPartOf: { '@type': 'WebSite', name: brand.displayName, url: brand.baseUrl },
    mainEntityOfPage: { '@type': 'WebPage', '@id': `${brand.baseUrl}/${locale}${pathname}` },
  };

  // The /k2/comparison aggregate page also emits FAQPage JSON-LD so AI search
  // engines extract structured Q&A. Each schema.org entity is rendered as its
  // own <script> tag (rather than a JSON array root) so third-party parsers
  // that assume a single-object root don't crash on @context lookup.
  const isComparison = slug === 'k2/comparison';
  const faqPage = isComparison ? buildComparisonFaqPage(locale, brand.baseUrl, pathname) : null;
  // Content is Velite-processed Markdown (trusted build-time source)
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(techArticle) }}
      />
      {faqPage && (
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(faqPage) }}
        />
      )}
      <article className="prose max-w-none min-w-0 flex-1">
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground mb-3 sm:mb-4">{post.title}</h1>
        {post.summary && (
          <p className="text-base sm:text-lg text-muted-foreground mb-6 sm:mb-8">{post.summary}</p>
        )}
        <div dangerouslySetInnerHTML={{ __html: post.content }} />
      </article>
    </>
  );
}
