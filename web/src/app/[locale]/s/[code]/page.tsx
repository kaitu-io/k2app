import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import InviteClient from '@/app/[locale]/s/[code]/InviteClient';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string; code: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'invite' });
  return {
    title: t('inviteLanding.friendGift'),
    description: t('inviteLanding.friendGiftDesc'),
  };
}

export default async function InvitePage({
  params,
}: {
  params: Promise<{ locale: string; code: string }>;
}) {
  const { locale: rawLocale, code } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  return (
    <div className="min-h-screen bg-gradient-to-br from-blue-50 via-white to-purple-50 dark:from-gray-900 dark:via-gray-800 dark:to-gray-900">
      <Header />
      <InviteClient code={code} />
      <Footer />
    </div>
  );
}
