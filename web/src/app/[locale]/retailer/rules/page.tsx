import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing, Link } from '@/i18n/routing';
import path from 'path';
import { Button } from '@/components/ui/button';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import MarkdownRenderer from '@/components/MarkdownRenderer';
import { Users } from 'lucide-react';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'admin' });
  return {
    title: t('retailerRules.title'),
    description: t('retailerRules.subtitle'),
  };
}

export default async function RetailerRulesPage({
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
    path.join(process.cwd(), 'public/legal/retailer-rules.md'),
    'utf-8'
  );

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />

      {/* Hero Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <div className="flex items-center justify-center mb-6">
            <div className="p-3 bg-purple-100 dark:bg-purple-900 rounded-full">
              <Users className="w-8 h-8 text-purple-600" />
            </div>
          </div>
          <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
            {t('admin.retailerRules.title')}
          </h1>
          <p className="text-xl text-gray-600 dark:text-gray-300 mb-8 max-w-3xl mx-auto">
            {t('admin.retailerRules.subtitle')}
          </p>
        </div>
      </section>

      {/* Main Content */}
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <div className="prose max-w-none">
          <MarkdownRenderer content={content} />
        </div>
      </div>

      {/* Contact Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
        <div className="max-w-4xl mx-auto text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-4">
            {t('admin.retailerRules.contact.title')}
          </h2>
          <p className="text-gray-600 dark:text-gray-300 mb-4">
            {t('admin.retailerRules.contact.content')}
          </p>
          <p className="text-purple-600 font-medium mb-8">{t('admin.retailerRules.contact.email')}</p>
          <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
            <Link href="/">
              <Button size="lg">
                {t('hero.routers.backToHome')}
              </Button>
            </Link>
            <Link href="/terms">
              <Button variant="outline" size="lg">
                {t('discovery.terms.title')}
              </Button>
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </div>
  );
}
