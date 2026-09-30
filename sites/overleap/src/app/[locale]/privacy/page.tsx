import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import LegalPage from '@/components/LegalPage';
import { pageMetadata } from '@/lib/metadata';

export const dynamic = 'force-static';

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'legal' });
  return pageMetadata(locale, '/privacy', { title: t('privacy.metaTitle'), description: t('privacy.subtitle') });
}

export default async function Page({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'legal' });
  return <LegalPage locale={locale} doc="privacy-policy" title={t('privacy.title')} subtitle={t('privacy.subtitle')} />;
}
