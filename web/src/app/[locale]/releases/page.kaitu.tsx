import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import ReleasesClient from './ReleasesClient';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

// Only this brand's locales — the layout 404s the rest anyway.
export function generateStaticParams() {
  return siteBrand().allowedLocales.map((locale) => ({ locale }));
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'releases' });

  return {
    title: t('title'),
    description: t('subtitle'),
  };
}

export default async function ReleasesPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;

  // ReleasesClient fetches /releases.json at runtime — a single-brand artifact
  // carrying kaitu-worded notes and kaitu installer URLs. Serving it from
  // another brand would publish a full kaitu changelog plus kaitu download
  // links under that brand's own domain. Gated until per-brand notes exist.
  if (!siteBrand().features.releaseNotes) {
    notFound();
  }

  setRequestLocale(locale);

  return <ReleasesClient />;
}
