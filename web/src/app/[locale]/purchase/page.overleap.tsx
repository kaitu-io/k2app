import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import OverleapPurchaseClient from './OverleapPurchaseClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'purchase' });
  return {
    title: t('purchase.title'),
    description: t('purchase.metaDescription'),
  };
}

// overleap 构建专属（page.overleap.tsx）：Stripe 订阅购买面。kaitu的 WordGate 流在 page.kaitu.tsx。
export default async function PurchasePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);
  return <OverleapPurchaseClient />;
}
