import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { Card } from '@/components/ui/card';
import InstallClient from '@/app/[locale]/install/InstallClient';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'install' });
  return {
    title: t('install.title'),
    description: t('install.allDownloadOptions'),
  };
}

export default async function InstallPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'install' });

  return (
    <div className="min-h-screen bg-gradient-to-b from-blue-50 to-white dark:from-blue-900/20 dark:to-gray-800">
      <Header />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Client island: device detection, download countdown, download state cards */}
        <InstallClient />

        {/* Help Section — static server-rendered content */}
        <Card className="p-6 mt-8 bg-gray-50 dark:bg-gray-800">
          <h4 className="font-semibold text-gray-900 dark:text-white mb-3">
            {t('install.needHelp')}
          </h4>
          <ul className="text-sm text-gray-600 dark:text-gray-300 space-y-2">
            <li>{t('install.helpBrowserBlock')}</li>
            <li>{t('install.helpWindowsInstall')}</li>
            <li>{t('install.helpMacosInstall')}</li>
            <li>{t('install.helpSecurityWarning')}</li>
          </ul>
        </Card>
      </div>

      <Footer />
    </div>
  );
}
