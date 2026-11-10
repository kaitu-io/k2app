import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { generateMetadata as generateBaseMetadata } from '../../metadata';
import { redirect, routing } from '@/i18n/routing';
import { formatUsd, routerOffer, ROUTER_EDITION_ON_SALE } from '@/lib/router-edition';
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
  return generateBaseMetadata(rawLocale, '/purchase/router', {
    title: t('edition.purchase.metaTitle'),
    description: t('edition.purchase.metaDescription', {
      price: formatUsd(offer.firstYear),
      renewal: formatUsd(offer.renewal),
    }),
  });
}

export default async function RouterPurchasePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;

  // 下架期间新购与续费（?plan=svc）都不开放——后台两个套餐已停用，下单只会失败。
  if (!ROUTER_EDITION_ON_SALE) redirect({ href: '/pricing', locale: rawLocale as Locale });

  setRequestLocale(rawLocale as Locale);

  return <RouterPurchaseClient />;
}
