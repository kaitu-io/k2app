import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import SiteShell from '@/components/SiteShell';
import JsonLd from '@/components/marketing/JsonLd';
import { findGuide, listGuides } from '@/lib/guides';
import { localeOf } from '@/lib/locale-param';
import { pageMetadata } from '@/lib/metadata';
import { INDEXED_LOCALES, SITE, isIndexedLocale } from '@/lib/site';

// Guides are written in English and served only under the indexed English
// locales; every other locale 404s rather than serving an untranslated page.
// The chrome strings here are therefore literal English, not messages.
export const dynamicParams = false;

type Props = { params: Promise<{ locale: string; slug: string }> };

export function generateStaticParams() {
  return INDEXED_LOCALES.flatMap((locale) => listGuides().map((g) => ({ locale, slug: g.slug })));
}

async function load(params: Props['params']) {
  const locale = await localeOf(params);
  const { slug } = await params;
  const guide = isIndexedLocale(locale) ? findGuide(slug) : undefined;
  if (!guide) notFound();
  return { locale, guide };
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale, guide } = await load(params);
  return pageMetadata(locale, `/guides/${guide.slug}`, {
    title: `${guide.title} | ${SITE.name}`,
    description: guide.description,
    ogType: 'article',
  });
}

export default async function GuidePage({ params }: Props) {
  const { locale, guide } = await load(params);
  setRequestLocale(locale);
  const url = `${SITE.baseUrl}/${locale}/guides/${guide.slug}`;

  const article = {
    '@context': 'https://schema.org',
    '@type': 'Article',
    headline: guide.title,
    description: guide.description,
    dateModified: guide.updated,
    inLanguage: 'en',
    mainEntityOfPage: url,
    publisher: { '@type': 'Organization', name: SITE.name, url: SITE.baseUrl },
  };
  const faqPage = guide.faq.length
    ? {
        '@context': 'https://schema.org',
        '@type': 'FAQPage',
        mainEntity: guide.faq.map((f) => ({
          '@type': 'Question',
          name: f.question,
          acceptedAnswer: { '@type': 'Answer', text: f.answer },
        })),
      }
    : null;

  return (
    <SiteShell locale={locale}>
      <article className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <header className="mb-10 border-b pb-8">
          <h1 className="text-4xl font-bold tracking-tight">{guide.title}</h1>
          <p className="mt-3 text-lg text-muted-foreground">{guide.description}</p>
          <p className="mt-4 text-sm text-muted-foreground">
            {'Updated '}
            <time dateTime={guide.updated}>{formatDate(guide.updated, locale)}</time>
          </p>
        </header>
        <div className="prose max-w-none">
          {/* The page owns the only <h1>; guide bodies start at ##. */}
          <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ h1: ({ children }) => <h2>{children}</h2> }}>
            {guide.body}
          </ReactMarkdown>
        </div>
      </article>
      <JsonLd data={article} />
      {faqPage && <JsonLd data={faqPage} />}
    </SiteShell>
  );
}

function formatDate(iso: string, locale: string): string {
  return new Date(`${iso}T00:00:00Z`).toLocaleDateString(locale, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    timeZone: 'UTC',
  });
}
