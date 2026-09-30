import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { Link } from '@/i18n/routing';
import SiteShell from '@/components/SiteShell';
import { Button } from '@/components/ui/button';
import { pageMetadata } from '@/lib/metadata';
import { SITE } from '@/lib/site';

// Placeholder home for the scaffold phase; replaced by the marketing pages
// (spec 2026-09-30-overleap-site-app-design.md §1, phase ③).
export const dynamic = 'force-static';

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'home' });
  return pageMetadata(locale, '', { title: `${t('metaTitle')} | ${SITE.name}`, description: t('metaDescription') });
}

export default async function Home({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'home' });
  return (
    <SiteShell locale={locale}>
      <section className="mx-auto max-w-3xl px-4 py-24 text-center sm:py-32">
        <h1 className="text-4xl font-bold tracking-tight sm:text-6xl">{t('title')}</h1>
        <p className="mx-auto mt-6 max-w-xl text-lg text-muted-foreground">{t('subtitle')}</p>
        <div className="mt-10 flex justify-center gap-3">
          <Button asChild size="lg">
            <Link href="/account">{t('ctaAccount')}</Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link href="/k2">{t('ctaDocs')}</Link>
          </Button>
        </div>
      </section>
    </SiteShell>
  );
}
