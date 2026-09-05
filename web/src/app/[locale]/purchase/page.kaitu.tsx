import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import PurchaseClient from './PurchaseClient';

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

export default async function PurchasePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);
  // kaitu构建专属（page.kaitu.tsx）：WordGate 下单流。overleap 的 Stripe 面在 page.overleap.tsx。
  return <PurchaseClient />;
}
