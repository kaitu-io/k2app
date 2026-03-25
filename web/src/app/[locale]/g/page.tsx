import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import GiftCodeClient from './GiftCodeClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata(): Promise<Metadata> {
  return {
    title: '兑换授权码 | Kaitu',
    description: '输入授权码，免费获取 Kaitu 会员',
  };
}

export default async function GiftCodeLandingPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <GiftCodeClient />
      <Footer />
    </div>
  );
}
