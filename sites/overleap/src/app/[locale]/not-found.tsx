import { getLocale, getTranslations } from 'next-intl/server';
import { Link } from '@/i18n/routing';
import SiteShell from '@/components/SiteShell';
import { Button } from '@/components/ui/button';

export default async function NotFound() {
  const locale = await getLocale();
  const t = await getTranslations({ locale, namespace: 'common' });
  return (
    <SiteShell locale={locale}>
      <section className="mx-auto max-w-xl px-4 py-32 text-center">
        <p className="text-sm font-semibold text-primary">{404}</p>
        <h1 className="mt-2 text-3xl font-bold">{t('notFoundTitle')}</h1>
        <p className="mt-3 text-muted-foreground">{t('notFoundBody')}</p>
        <Button asChild className="mt-8">
          <Link href="/">{t('backHome')}</Link>
        </Button>
      </section>
    </SiteShell>
  );
}
