import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { Link, routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { Hero } from '../_components/Hero';
import { Step1Hardware } from '../_components/Step1Hardware';
import { Step2InstallOS } from '../_components/Step2InstallOS';
import { Step3InstallK2r } from '../_components/Step3InstallK2r';
import { Step4Setup } from '../_components/Step4Setup';
import { RoutersFAQ } from '../_components/RoutersFAQ';
import { VsClient } from '../_components/VsClient';

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
    title: t('edition.diy.metaTitle'),
    description: t('edition.diy.metaDescription'),
  };
}

export default async function RoutersDiyPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;

  // The DIY router surface is kaitu-only (Brand.features.routers). Without
  // this gate the overleap deployment would serve 开途-branded router products —
  // the exact leak tests/brand-guard.test.ts allowlists this directory against.
  if (!siteBrand().features.routers) {
    notFound();
  }

  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'routers' });

  return (
    <div className="min-h-screen bg-background">
      <Header />
      {/* 自备教程的读者多半只是想省事：先给一条回路由器版成品的路，
          再往下讲刷机 —— 免得他们读完四步才发现有更省心的选项。 */}
      <div className="bg-primary/10 border-b border-primary/20 px-4 sm:px-6 lg:px-8 py-3">
        <div className="max-w-7xl mx-auto flex flex-wrap items-center justify-center gap-2 text-sm text-center">
          <span className="text-foreground/90">{t('edition.diy.upsell')}</span>
          <Link href="/routers" className="font-semibold text-primary underline underline-offset-4 hover:text-primary/80">
            {t('edition.diy.upsellCta')}
          </Link>
        </div>
      </div>
      <Hero />
      <Step1Hardware locale={locale} />
      <Step2InstallOS />
      <Step3InstallK2r />
      <Step4Setup />
      <RoutersFAQ />
      <VsClient />
      <Footer />
    </div>
  );
}
