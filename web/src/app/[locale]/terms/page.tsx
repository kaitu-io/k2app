import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing, Link } from '@/i18n/routing';
import path from 'path';
import { Button } from '@/components/ui/button';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import MarkdownRenderer from '@/components/MarkdownRenderer';
import { Scale } from 'lucide-react';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'discovery' });
  return {
    title: t('terms.title'),
    description: t('terms.subtitle'),
  };
}

export default async function TermsPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale });

  const { readFile } = await import('fs/promises');
  const content = await readFile(
    path.join(process.cwd(), 'public/legal/terms-of-service.md'),
    'utf-8'
  );

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />

      {/* Hero Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <div className="flex items-center justify-center mb-6">
            <div className="p-3 bg-green-100 dark:bg-green-900 rounded-full">
              <Scale className="w-8 h-8 text-green-600" />
            </div>
          </div>
          <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
            {t('discovery.terms.title')}
          </h1>
          <p className="text-xl text-gray-600 dark:text-gray-300 mb-8 max-w-3xl mx-auto">
            {t('discovery.terms.subtitle')}
          </p>
        </div>
      </section>

      {/* Main Content */}
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <div className="prose prose-gray dark:prose-invert max-w-none">
          <MarkdownRenderer content={content} />
        </div>
      </div>

      {/* Contact Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
        <div className="max-w-4xl mx-auto text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-4">
            {t('discovery.terms.contact.title')}
          </h2>
          <p className="text-gray-600 dark:text-gray-300 mb-4">
            {t('discovery.terms.contact.content')}
          </p>
          <p className="text-green-600 font-medium mb-8">{t('discovery.terms.contact.email')}</p>
          <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
            <Link href="/">
              <Button size="lg">
                {t('hero.routers.backToHome')}
              </Button>
            </Link>
            <Link href="/privacy">
              <Button variant="outline" size="lg">
                {t('discovery.privacy.title')}
              </Button>
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </div>
  );
}
