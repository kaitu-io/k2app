/**
 * /k2/[[...path]] — k2 protocol docs. `/k2` renders the `k2` index post.
 * Content is Velite-compiled markdown from this repo (trusted build-time HTML).
 */
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import { findK2Post, k2Slugs } from '@/lib/k2-posts';
import { pageMetadata } from '@/lib/metadata';
import { localeOf } from '@/lib/locale-param';
import { LOCALES, SITE } from '@/lib/site';

export const dynamic = 'force-static';
export const dynamicParams = false;

type Params = { locale: string; path?: string[] };

const slugOf = (path?: string[]) => (path?.length ? `k2/${path.join('/')}` : 'k2');
const pathnameOf = (path?: string[]) => `/${slugOf(path)}`;

/**
 * /k2/comparison also emits FAQPage JSON-LD (AI search engines extract the Q&A).
 * Mirrors the comparison sections in content/<locale>/k2/comparison.md.
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

function comparisonFaq(locale: string, url: string) {
  const qas = locale === 'ja' ? COMPARISON_QAS_JA : COMPARISON_QAS_EN;
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    '@id': `${url}#faq`,
    mainEntity: qas.map((qa) => ({ '@type': 'Question', name: qa.question, acceptedAnswer: { '@type': 'Answer', text: qa.answer } })),
  };
}

/** Docs open with their own `# Title`; the page already renders the one <h1>. */
function stripLeadingH1(html: string): string {
  return html.replace(/^\s*<h1[^>]*>[\s\S]*?<\/h1>/, '');
}

export function generateStaticParams(): Params[] {
  return LOCALES.flatMap((locale) =>
    k2Slugs().map((slug) => ({ locale, path: slug === 'k2' ? undefined : slug.slice(3).split('/') })),
  );
}

export async function generateMetadata({ params }: { params: Promise<Params> }): Promise<Metadata> {
  const locale = await localeOf(params);
  const { path } = await params;
  const post = findK2Post(locale, slugOf(path));
  if (!post) return {};
  return pageMetadata(locale, pathnameOf(path), {
    title: `${post.title} | ${SITE.name}`,
    description: post.summary ?? '',
    ogType: 'article',
  });
}

export default async function K2Page({ params }: { params: Promise<Params> }) {
  const locale = await localeOf(params);
  const { path } = await params;
  setRequestLocale(locale);
  const post = findK2Post(locale, slugOf(path));
  if (!post) notFound();

  const url = `${SITE.baseUrl}/${locale}${pathnameOf(path)}`;
  const article = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    headline: post.title,
    description: post.summary ?? '',
    url,
    datePublished: post.date,
    dateModified: post.date,
    inLanguage: locale,
    author: { '@type': 'Organization', name: SITE.name, url: SITE.baseUrl },
    publisher: { '@type': 'Organization', name: SITE.name, url: SITE.baseUrl },
  };
  const jsonLd = [article, ...(post.slug === 'k2/comparison' ? [comparisonFaq(locale, url)] : [])];

  return (
    <>
      {jsonLd.map((d, i) => (
        <script key={i} type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(d).replace(/</g, '\\u003c') }} />
      ))}
      <article className="prose max-w-none">
        <h1>{post.title}</h1>
        {post.summary && <p className="text-lg text-muted-foreground">{post.summary}</p>}
        <div dangerouslySetInnerHTML={{ __html: stripLeadingH1(post.content) }} />
      </article>
    </>
  );
}
