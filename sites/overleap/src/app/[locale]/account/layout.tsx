import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import AccountFrame from './AccountFrame';
import { pageMetadata } from '@/lib/metadata';
import { SITE } from '@/lib/site';

type Props = { children: React.ReactNode; params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'account' });
  return pageMetadata(locale, '/account', { title: `${t('metaTitle')} | ${SITE.name}`, description: t('subtitle'), index: false });
}

export default async function AccountLayout({ children, params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  return (
    <SiteShell locale={locale}>
      <AccountFrame>{children}</AccountFrame>
    </SiteShell>
  );
}
