import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import RouterPurchaseClient from './RouterPurchaseClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const t = await getTranslations({ locale: rawLocale as Locale, namespace: 'routers' });
  return {
    title: t('edition.purchase.metaTitle'),
    description: t('edition.purchase.metaDescription'),
  };
}

export default async function RouterPurchasePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;

  // 路由器版结账页只属于一个品牌（features.routers）——另一品牌的构建里
  // 该目录不存在对应 page 文件，直接原生 404；这里的门是防御第二层。
  if (!siteBrand().features.routers) {
    notFound();
  }

  setRequestLocale(rawLocale as Locale);

  return <RouterPurchaseClient />;
}
