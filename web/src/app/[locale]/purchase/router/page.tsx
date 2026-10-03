import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { formatUsd, routerOffer } from '@/lib/router-edition';
import RouterPurchaseClient from './RouterPurchaseClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const t = await getTranslations({ locale: rawLocale as Locale, namespace: 'routers' });
  const offer = routerOffer();
  return {
    title: t('edition.purchase.metaTitle'),
    description: t('edition.purchase.metaDescription', {
      price: formatUsd(offer.firstYear),
      renewal: formatUsd(offer.renewal),
    }),
  };
}

export default async function RouterPurchasePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;

  setRequestLocale(rawLocale as Locale);

  return <RouterPurchaseClient />;
}
