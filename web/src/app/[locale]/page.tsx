import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS } from '@/lib/constants';
import { Link } from '@/i18n/routing';
import NextLink from 'next/link';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import HomeClient from './HomeClient';
import {
  Download,
  ExternalLink,
  Smartphone,
  Monitor,
  Router
} from 'lucide-react';

export const dynamic = 'force-static';

/**
 * Generate metadata for the homepage (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'hero' });

  return {
    title: `Kaitu - ${t('hero.title')} ${t('hero.networkProxy')} ${t('hero.solution')}`,
    description: t('hero.description'),
  };
}

/**
 * Homepage Server Component.
 *
 * Converted from CSR ("use client") to SSR/SSG. Interactive parts
 * (MPTCPVisualization canvas animation) are delegated to HomeClient.
 *
 * Uses Next.js 15 async params pattern: params is a Promise that must
 * be awaited before accessing its properties.
 */
export default async function Home({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'hero' });
  const tNav = await getTranslations({ locale, namespace: 'nav' });

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />

      {/* Hero Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto text-center">
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-gray-900 dark:text-white mb-6">
            {t('hero.title')}
            <span className="text-blue-600"> {t('hero.networkProxy')}</span>
            <br />{t('hero.solution')}
          </h1>
          <p className="text-xl text-gray-600 dark:text-gray-300 mb-8 max-w-3xl mx-auto">
            {t('hero.description')}
          </p>
            <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4 max-w-md sm:max-w-2xl mx-auto">
            <Link href="/install" className="w-full sm:flex-1">
              <Button size="lg" className="w-full min-w-[200px]">
                <Download className="w-5 h-5 mr-2" />
                {t('hero.downloadClient')}
              </Button>
            </Link>
            <Link href="/purchase" className="w-full sm:flex-1">
              <Button variant="destructive" size="lg" className="w-full min-w-[200px] font-semibold shadow-md hover:shadow-lg transition-all duration-200">
                <Router className="w-5 h-5 mr-2" />
                {tNav('nav.purchase')}
              </Button>
            </Link>
          </div>
        </div>
      </section>

      {/* Protocol Technology Showcase */}
      <section className="py-20 px-4 sm:px-6 lg:px-8 bg-gradient-to-b from-blue-50 to-white dark:from-blue-900/20 dark:to-gray-800">
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-4">{t('security.title')}</h2>
            <p className="text-gray-600 dark:text-gray-300 text-lg max-w-3xl mx-auto">
              {t('security.description')}
            </p>
          </div>

          {/* Core Protocol Technology - MPTCP */}
          <div className="mb-16">
            <Card className="p-8 bg-gradient-to-r from-blue-600 to-indigo-600 text-white relative overflow-hidden">
              <div className="absolute top-0 right-0 w-32 h-32 bg-white/10 rounded-full -translate-y-16 translate-x-16"></div>
              <div className="absolute bottom-0 left-0 w-24 h-24 bg-white/5 rounded-full translate-y-12 -translate-x-12"></div>
              <div className="relative z-10">
                <div className="w-16 h-16 mb-6 bg-white/20 rounded-full flex items-center justify-center">
                  <span className="text-2xl">{t('security.icon')}</span>
                </div>
                <h3 className="text-2xl font-bold mb-4">{t('security.caTech.title')}</h3>
                <p className="text-blue-100 text-lg mb-6">
                  {t('security.caTech.description')}
                </p>
                <div className="grid md:grid-cols-2 gap-6">
                  <div>
                    <h4 className="font-semibold mb-2">{t('security.caTech.advantages.title')}</h4>
                    <ul className="text-blue-100 space-y-1">
                      <li>{t('security.caTech.advantages.bullet')} {t('security.caTech.advantages.item1')}</li>
                      <li>{t('security.caTech.advantages.bullet')} {t('security.caTech.advantages.item2')}</li>
                      <li>{t('security.caTech.advantages.bullet')} {t('security.caTech.advantages.item3')}</li>
                      <li>{t('security.caTech.advantages.bullet')} {t('security.caTech.advantages.item4')}</li>
                    </ul>
                  </div>
                  <div>
                    <h4 className="font-semibold mb-2">{t('security.caTech.adaptive.title')}</h4>
                    <ul className="text-blue-100 space-y-1">
                      <li>{t('security.caTech.adaptive.bullet')} {t('security.caTech.adaptive.item1')}</li>
                      <li>{t('security.caTech.adaptive.bullet')} {t('security.caTech.adaptive.item2')}</li>
                      <li>{t('security.caTech.adaptive.bullet')} {t('security.caTech.adaptive.item3')}</li>
                      <li>{t('security.caTech.adaptive.bullet')} {t('security.caTech.adaptive.item4')}</li>
                    </ul>
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* MPTCP Visualization — rendered by client component */}
          <div className="mb-16">
            <h3 className="text-2xl font-bold text-center text-gray-900 dark:text-white mb-4">
              {t('security.visualization.title')}
            </h3>
            <p className="text-gray-600 dark:text-gray-300 text-center mb-8 max-w-2xl mx-auto">
              {t('security.visualization.subtitle')}
            </p>
            <HomeClient />
            <p className="text-gray-500 dark:text-gray-400 text-sm text-center mt-6 max-w-3xl mx-auto">
              {t('security.visualization.description')}
            </p>
          </div>

          {/* Protocol Features Grid */}
          <div className="mb-16">
            <h3 className="text-2xl font-bold text-center text-gray-900 dark:text-white mb-8">
              {t('security.features.title')}
            </h3>
            <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-blue-500">
                <div className="w-12 h-12 mb-4 bg-blue-100 dark:bg-blue-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.multipath.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.multipath.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.multipath.description')}
                </p>
              </Card>

              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-green-500">
                <div className="w-12 h-12 mb-4 bg-green-100 dark:bg-green-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.encryption.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.encryption.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.encryption.description')}
                </p>
              </Card>

              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-purple-500">
                <div className="w-12 h-12 mb-4 bg-purple-100 dark:bg-purple-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.smartRouting.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.smartRouting.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.smartRouting.description')}
                </p>
              </Card>

              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-orange-500">
                <div className="w-12 h-12 mb-4 bg-orange-100 dark:bg-orange-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.disguise.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.disguise.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.disguise.description')}
                </p>
              </Card>

              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-cyan-500">
                <div className="w-12 h-12 mb-4 bg-cyan-100 dark:bg-cyan-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.failover.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.failover.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.failover.description')}
                </p>
              </Card>

              <Card className="p-6 hover:shadow-lg transition-shadow border-t-4 border-yellow-500">
                <div className="w-12 h-12 mb-4 bg-yellow-100 dark:bg-yellow-900/50 rounded-lg flex items-center justify-center">
                  <span className="text-2xl">{t('security.features.performance.icon')}</span>
                </div>
                <h4 className="font-bold text-gray-900 dark:text-white mb-2">{t('security.features.performance.title')}</h4>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t('security.features.performance.description')}
                </p>
              </Card>
            </div>
          </div>

        </div>
      </section>

      {/* Download Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto text-center">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-8">{t('download.title')}</h2>
          <p className="text-gray-600 dark:text-gray-300 text-lg mb-12">
            {t('download.subtitle')}
          </p>

          <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6 max-w-7xl mx-auto">
            <Card className="p-6 hover:shadow-lg transition-shadow group">
              <Smartphone className="w-12 h-12 text-blue-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold mb-2">{t('download.platforms.iosLabel')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-4">{t('download.platforms.ios')}</p>
              {DOWNLOAD_LINKS.ios ? (
                <NextLink href={DOWNLOAD_LINKS.ios} target="_blank" rel="noopener noreferrer">
                  <Button variant="outline" size="sm" className="group-hover:bg-blue-50">
                    <ExternalLink className="w-4 h-4 mr-2" />
                    {t('download.downloadButton')}
                  </Button>
                </NextLink>
              ) : (
                <Button variant="outline" size="sm" disabled className="opacity-50 cursor-not-allowed">
                  <span className="mr-2">{"⏳"}</span>
                  {t('download.comingSoon')}
                </Button>
              )}
            </Card>

            <Card className="p-6 hover:shadow-lg transition-shadow group">
              <Smartphone className="w-12 h-12 text-green-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold mb-2">{t('download.platforms.androidLabel')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-4">{t('download.platforms.android')}</p>
              {DOWNLOAD_LINKS.android ? (
                <NextLink href={DOWNLOAD_LINKS.android} target="_blank" rel="noopener noreferrer">
                  <Button variant="outline" size="sm" className="group-hover:bg-green-50">
                    <ExternalLink className="w-4 h-4 mr-2" />
                    {t('download.downloadButton')}
                  </Button>
                </NextLink>
              ) : (
                <Button variant="outline" size="sm" disabled className="opacity-50 cursor-not-allowed">
                  <span className="mr-2">{"⏳"}</span>
                  {t('download.comingSoon')}
                </Button>
              )}
            </Card>

            <Card className="p-6 hover:shadow-lg transition-shadow group">
              <Monitor className="w-12 h-12 text-purple-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold mb-2">{t('download.platforms.windowsLabel')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-4">{t('download.platforms.windows')}</p>
              <NextLink href={DOWNLOAD_LINKS.windows} target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm" className="group-hover:bg-purple-50">
                  <ExternalLink className="w-4 h-4 mr-2" />
                  {t('download.downloadButton')}
                </Button>
              </NextLink>
            </Card>

            <Card className="p-6 hover:shadow-lg transition-shadow group">
              <Monitor className="w-12 h-12 text-gray-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold mb-2">{t('download.platforms.macosLabel')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-4">{t('download.platforms.macos')}</p>
              <NextLink href={DOWNLOAD_LINKS.macos} target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm" className="group-hover:bg-gray-50">
                  <ExternalLink className="w-4 h-4 mr-2" />
                  {t('download.downloadButton')}
                </Button>
              </NextLink>
            </Card>
          </div>
        </div>
      </section>

      <Footer />
    </div>
  );
}
