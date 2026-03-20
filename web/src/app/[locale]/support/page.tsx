import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { generateMetadata as generateBaseMetadata } from '../metadata';
import SupportClient from './SupportClient';

type Locale = (typeof routing.locales)[number];

const FAQ_ITEMS = ['multiDevice', 'connectionFailed', 'purchase', 'platforms', 'childSafety'] as const;

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const base = generateBaseMetadata(locale);
  const t = await getTranslations({ locale, namespace: 'guide-parents' });

  const title = `${t('hero.title')} | Kaitu`;
  const description = t('hero.subtitle');

  return {
    ...base,
    title,
    description,
    openGraph: {
      ...(base.openGraph as Record<string, unknown>),
      title,
      description,
    },
    twitter: {
      ...(base.twitter as Record<string, unknown>),
      title,
      description,
    },
  };
}

export default async function SupportPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'guide-parents' });

  const jsonLd = JSON.stringify({
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: FAQ_ITEMS.map((item) => ({
      '@type': 'Question',
      name: t(`faq.items.${item}.question`),
      acceptedAnswer: {
        '@type': 'Answer',
        text: t(`faq.items.${item}.answer`),
      },
    })),
  });

  return (
    <>
      {/* FAQPage JSON-LD — i18n translations are trusted server-side content */}
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: jsonLd }}
      />
      <SupportClient />
    </>
  );
}
