import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing, Link } from '@/i18n/routing';
import path from 'path';
import { Button } from '@/components/ui/button';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import MarkdownRenderer from '@/components/MarkdownRenderer';
import { renderLegalDoc } from '@/lib/legal';
import { UserX } from 'lucide-react';

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
    title: t('deleteAccount.title'),
    description: t('deleteAccount.subtitle'),
  };
}

/**
 * Public account-deletion instructions.
 *
 * Google Play requires a deletion route that works for someone who has already
 * uninstalled the app, so this page must stay reachable without signing in and
 * without the app installed — its URL is filed in the Play Data safety form.
 * Play also requires the page to name the app or developer, show the steps
 * prominently, and state what is deleted vs retained; all three live in
 * public/legal/delete-account.md. Apple's 5.1.1(v) is a different requirement,
 * satisfied by the in-app flow this page documents rather than by this page.
 */
export default async function DeleteAccountPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale });

  const { readFile } = await import('fs/promises');
  const raw = await readFile(
    path.join(process.cwd(), 'public/legal/delete-account.md'),
    'utf-8'
  );
  const content = renderLegalDoc(raw, locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />

      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <div className="flex items-center justify-center mb-6">
            <div className="p-3 bg-secondary/15 rounded-full">
              <UserX className="w-8 h-8 text-blue-600" />
            </div>
          </div>
          <h1 className="text-4xl sm:text-5xl font-bold text-foreground mb-4">
            {t('discovery.deleteAccount.title')}
          </h1>
          <p className="text-xl text-muted-foreground mb-8 max-w-3xl mx-auto">
            {t('discovery.deleteAccount.subtitle')}
          </p>
        </div>
      </section>

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <div className="prose max-w-none">
          <MarkdownRenderer content={content} />
        </div>
      </div>

      <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
        <div className="max-w-4xl mx-auto text-center">
          <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
            <Link href="/">
              <Button size="lg">
                {t('nav.nav.backToHome')}
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
