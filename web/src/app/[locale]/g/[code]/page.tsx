import { routing } from '@/i18n/routing';
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import RedeemClient from './RedeemClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata(): Promise<Metadata> {
  return { title: '兑换授权码 | Kaitu' };
}

export default async function GiftCodeDirectPage({
  params,
}: {
  params: Promise<{ code: string; locale: string }>;
}) {
  const { code, locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <RedeemClient code={code} />
      <Footer />
    </div>
  );
}
