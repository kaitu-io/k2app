import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { formatUsd, routerOffer } from '@/lib/router-edition';
import { EditionHero } from './_components/EditionHero';
import { EditionHowItWorks } from './_components/EditionHowItWorks';
import { EditionPricing } from './_components/EditionPricing';
import { EditionFAQ } from './_components/EditionFAQ';

type Locale = (typeof routing.locales)[number];

// 每小时重生成：预售价与「{date} 起发货」文案按 ROUTER_PRESALE.shipsFrom 自动切换，
// 发售日不需要重新部署（force-static 会把预售文案钉到下一次部署）。
export const revalidate = 3600;

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
      price: formatUsd(routerOffer().firstYear),
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
  const t = await getTranslations({ locale, namespace: 'routers' });

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <EditionHero t={t} locale={locale} />
      <EditionHowItWorks t={t} />
      <EditionPricing t={t} locale={locale} />
      <EditionFAQ t={t} locale={locale} />
      <Footer />
    </div>
  );
}
