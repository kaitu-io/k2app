import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { Card } from '@/components/ui/card';
import { Github, Calendar } from 'lucide-react';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import CountdownTimer from './CountdownTimer';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

const OPENSOURCE_DATE_ISO = '2026-06-04T00:00:00Z';

/**
 * Generate metadata for the opensource page (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'theme' });

  return {
    title: t('opensource.title'),
    description: t('opensource.subtitle'),
  };
}

/**
 * Opensource page Server Component — SSR-converted from client component.
 *
 * Static content (hero, countdown card frame, why open source) rendered on server.
 * CountdownTimer client island handles the live countdown with useState/setInterval.
 * Uses async params per Next.js 15 pattern.
 */
export default async function OpenSourcePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale });

  return (
    <div className="min-h-screen bg-gradient-to-br from-blue-50 via-white to-purple-50 dark:from-gray-900 dark:via-gray-800 dark:to-gray-900">
      <Header />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Hero Section */}
        <div className="text-center mb-12">
          <div className="inline-flex items-center justify-center p-4 bg-gradient-to-br from-green-100 to-blue-100 dark:from-green-900/30 dark:to-blue-900/30 rounded-full mb-6">
            <Github className="w-12 h-12 text-green-600 dark:text-green-400" />
          </div>

          <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
            {t('theme.opensource.title')}
          </h1>

          <p className="text-xl text-gray-600 dark:text-gray-300 mb-2">
            {t('theme.opensource.subtitle')}
          </p>

          <p className="text-lg text-gray-500 dark:text-gray-400">
            {t('theme.opensource.description')}
          </p>
        </div>

        {/* Countdown Card */}
        <Card className="p-8 mb-8 border-2 border-blue-200 dark:border-blue-800 bg-white dark:bg-gray-800 shadow-2xl">
          <div className="text-center mb-8">
            <div className="flex items-center justify-center mb-4">
              <Calendar className="w-6 h-6 text-blue-600 dark:text-blue-400 mr-2" />
              <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                {t('theme.opensource.targetDate')}
              </h2>
            </div>
            <p className="text-3xl font-bold text-blue-600 dark:text-blue-400">
              {t('theme.opensource.targetDateValue')}
            </p>
          </div>

          {/* Client island: handles live countdown with useState/setInterval */}
          <CountdownTimer targetDateISO={OPENSOURCE_DATE_ISO} />
        </Card>

        {/* Why Open Source */}
        <Card className="p-8 bg-gradient-to-r from-green-50 to-blue-50 dark:from-green-900/20 dark:to-blue-900/20 border-2 border-green-200 dark:border-green-800">
          <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-6 text-center">
            {t('theme.opensource.whyTitle')}
          </h3>

          <div className="grid md:grid-cols-2 gap-6">
            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-green-600 rounded-full flex items-center justify-center text-white font-bold">
                {"1"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason1Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason1Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-blue-600 rounded-full flex items-center justify-center text-white font-bold">
                {"2"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason2Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason2Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-purple-600 rounded-full flex items-center justify-center text-white font-bold">
                {"3"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason3Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason3Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-pink-600 rounded-full flex items-center justify-center text-white font-bold">
                {"4"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason4Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason4Desc')}
                </p>
              </div>
            </div>
          </div>
        </Card>
      </div>

      <Footer />
    </div>
  );
}
