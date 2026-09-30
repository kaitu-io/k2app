import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { Suspense } from 'react';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import LoginForm from './LoginForm';
import { pageMetadata } from '@/lib/metadata';
import { SITE } from '@/lib/site';

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'auth' });
  return pageMetadata(locale, '/login', { title: `${t('metaTitle')} | ${SITE.name}`, description: t('subtitle'), index: false });
}

export default async function LoginPage({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  return (
    <SiteShell locale={locale}>
      <div className="mx-auto flex max-w-md flex-col px-4 py-16 sm:py-24">
        {/* useSearchParams (?next=) requires a Suspense boundary for static rendering. */}
        <Suspense>
          <LoginForm />
        </Suspense>
      </div>
    </SiteShell>
  );
}
