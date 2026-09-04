import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { Hero } from './_components/Hero';
import { Step1Hardware } from './_components/Step1Hardware';
import { Step2InstallOS } from './_components/Step2InstallOS';
import { Step3InstallK2r } from './_components/Step3InstallK2r';
import { Step4Setup } from './_components/Step4Setup';
import { RoutersFAQ } from './_components/RoutersFAQ';
import { VsClient } from './_components/VsClient';
import { PresaleFooterCards } from './_components/PresaleFooterCards';

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
    title: t('title'),
    description: t('subtitle'),
  };
}

export default async function RoutersPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;

  // The routers presale surface is kaitu-only (Brand.features.routers). Without
  // this gate the overleap deployment would serve 开途-branded router products —
  // the exact leak tests/brand-guard.test.ts allowlists this directory against.
  if (!siteBrand().features.routers) {
    notFound();
  }

  setRequestLocale(locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <Hero />
      <Step1Hardware locale={locale} />
      <Step2InstallOS />
      <Step3InstallK2r />
      <Step4Setup />
      <RoutersFAQ />
      <VsClient />
      <PresaleFooterCards />
      <Footer />
    </div>
  );
}
