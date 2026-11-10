import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import GiftCodeClient from './GiftCodeClient';
import { giftCodeMetadata } from '@/lib/gift-code-meta';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  return giftCodeMetadata(locale);
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
