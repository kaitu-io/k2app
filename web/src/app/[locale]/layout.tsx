import { NextIntlClientProvider } from 'next-intl';
import { getMessages, setRequestLocale } from 'next-intl/server';
import { notFound } from 'next/navigation';
import { routing } from '@/i18n/routing';
import { AuthProvider } from "@/contexts/AuthContext";
import { AppConfigProvider } from "@/contexts/AppConfigContext";
import { Toaster } from "@/components/ui/sonner";
import LanguageDetectionBanner from '@/components/LanguageDetectionBanner';
import CookieConsent from '@/components/CookieConsent';
import { EmbedThemeProvider } from '@/components/providers/EmbedThemeProvider';
import { LocaleProvider } from '@/components/providers/LocaleProvider';
import { generateMetadata as generatePageMetadata } from './metadata';
import { Metadata } from 'next';
import { Suspense } from 'react';
import { Inter } from "next/font/google";
import "../globals.css";

const inter = Inter({ subsets: ["latin"] });

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

export async function generateMetadata({
  params
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  return generatePageMetadata(locale);
}

export default async function LocaleLayout({
  children,
  params
}: {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  // Ensure that the incoming `locale` is valid
  if (!routing.locales.includes(locale as (typeof routing.locales)[number])) {
    notFound();
  }

  // Enable static rendering
  setRequestLocale(locale as (typeof routing.locales)[number]);

  // Providing all messages to the client
  // side is the easiest way to get started
  const messages = await getMessages();


  return (
    <html lang={locale} suppressHydrationWarning>
      <body className={inter.className} suppressHydrationWarning>
        <NextIntlClientProvider messages={messages}>
          <LocaleProvider locale={locale}>
            <Suspense fallback={null}>
              <EmbedThemeProvider>
                <AppConfigProvider>
                  <AuthProvider>
                    <LanguageDetectionBanner />
                    {children}
                    <Toaster />
                    <CookieConsent />
                  </AuthProvider>
                </AppConfigProvider>
              </EmbedThemeProvider>
            </Suspense>
          </LocaleProvider>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}