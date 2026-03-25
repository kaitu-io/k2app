import { api } from '@/lib/api';
import { notFound } from 'next/navigation';
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
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  let key = null;
  try {
    key = await api.getLicenseKey(code);
  } catch {
    notFound();
  }

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <RedeemClient initialKey={key} code={code} />
      <Footer />
    </div>
  );
}
