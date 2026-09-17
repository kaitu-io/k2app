import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { EDITION_PRICE_CENTS, formatUsd } from '@/lib/router-edition';
import { EditionHero } from './_components/EditionHero';
import { EditionHowItWorks } from './_components/EditionHowItWorks';
import { EditionPricing } from './_components/EditionPricing';
import { EditionFAQ } from './_components/EditionFAQ';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'routers' });
  return {
    title: t('edition.product.metaTitle'),
    description: t('edition.product.metaDescription', {
      price: formatUsd(EDITION_PRICE_CENTS.firstYear),
    }),
  };
}

export default async function RoutersEditionPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;

  // The router edition product surface is kaitu-only (Brand.features.routers).
  // Without this gate the overleap deployment would serve 开途-branded router
  // products — the exact leak tests/brand-guard.test.ts allowlists this
  // directory against.
  if (!siteBrand().features.routers) {
    notFound();
  }

  setRequestLocale(locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <EditionHero />
      <EditionHowItWorks />
      <EditionPricing />
      <EditionFAQ />
      <Footer />
    </div>
  );
}
