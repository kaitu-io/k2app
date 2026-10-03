import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { Suspense } from 'react';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import PurchaseClient from './PurchaseClient';
import { pageMetadata } from '@/lib/metadata';
import { priceVars } from '@/lib/pricing';
import { SITE } from '@/lib/site';
import ChatWidgetLazy from '@/components/chat/ChatWidgetLazy';

export const dynamic = 'force-static';

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'purchase' });
  const tp = await getTranslations({ locale, namespace: 'pricing' });
  return pageMetadata(locale, '/purchase', {
    title: `${t('title')} | ${SITE.name}`,
    description: tp('metaDescription', { brand: SITE.name, ...priceVars(locale) }),
  });
}

// No embed mode: the apps buy in-app (Stripe panel / IAP); this page is the browser checkout.
export default async function PurchasePage({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  return (
    <SiteShell locale={locale}>
      {/* useSearchParams (?plan=, ?checkout=cancelled) requires a Suspense boundary for static rendering. */}
      <Suspense>
        <PurchaseClient />
      </Suspense>
      <ChatWidgetLazy />
    </SiteShell>
  );
}
