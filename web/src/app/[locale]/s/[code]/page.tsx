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
    <div className="min-h-screen">
      <Header />
      <div
        className="bg-gradient-to-br from-white via-blue-50/50 to-purple-50/50"
        style={{
          '--card': '#ffffff',
          '--card-foreground': '#111827',
          '--border': '#e5e7eb',
          '--muted': '#f3f4f6',
          '--muted-foreground': '#6b7280',
          '--input': '#e5e7eb',
          '--primary': '#4f46e5',
          '--primary-foreground': '#ffffff',
          '--ring': '#4f46e5',
        } as React.CSSProperties}
      >
        <InviteClient code={code} />
      </div>
      <Footer />
    </div>
  );
}
