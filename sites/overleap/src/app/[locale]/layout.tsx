import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { Inter, JetBrains_Mono } from 'next/font/google';
import Script from 'next/script';
import { NextIntlClientProvider } from 'next-intl';
import { getMessages, setRequestLocale } from 'next-intl/server';
import { AuthProvider } from '@/contexts/AuthContext';
import { Toaster } from '@/components/ui/sonner';
import { LOCALES, LOCALE_META, SITE } from '@/lib/site';
import { ICONS } from '@/lib/metadata';
import { CHAT_RESUME_SCRIPT } from '@/components/chat/resume-script';
import '../globals.css';

const inter = Inter({ subsets: ['latin'], variable: '--font-inter' });
const mono = JetBrains_Mono({ subsets: ['latin'], variable: '--font-mono' });

export function generateStaticParams() {
  return LOCALES.map((locale) => ({ locale }));
}

export const metadata: Metadata = {
  metadataBase: new URL(SITE.baseUrl),
  applicationName: SITE.name,
  icons: ICONS,
};

export default async function LocaleLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}) {
  // Scanner paths (/wp-login.php, /.env …) land here as `locale`; localeOf 404s
  // them before anything feeds them to Intl (RangeError).
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const messages = await getMessages();

  return (
    <html lang={locale} dir={LOCALE_META[locale].dir} className="dark" suppressHydrationWarning>
      <body className={`${inter.variable} ${mono.variable} font-sans`}>
        {/* Moves a chat resume token (#chat=… in support reply emails) out of the URL
            before anything else reads it. See components/chat/resume-script.ts. */}
        <Script id="chat-resume-strip" strategy="beforeInteractive">
          {CHAT_RESUME_SCRIPT}
        </Script>
        <NextIntlClientProvider messages={messages}>
          <AuthProvider>
            {children}
            <Toaster />
          </AuthProvider>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
