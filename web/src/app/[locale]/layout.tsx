import { NextIntlClientProvider } from 'next-intl';
import { getMessages, setRequestLocale } from 'next-intl/server';
import { notFound } from 'next/navigation';
import { routing } from '@/i18n/routing';
import { AuthProvider } from "@/contexts/AuthContext";
import { AppConfigProvider } from "@/contexts/AppConfigContext";
import { Toaster } from "@/components/ui/sonner";
import BrowserWarningBar from '@/components/BrowserWarningBar';
import LanguageDetectionBanner from '@/components/LanguageDetectionBanner';
import CookieConsent from '@/components/CookieConsent';
import { EmbedThemeProvider } from '@/components/providers/EmbedThemeProvider';
import { LocaleProvider } from '@/components/providers/LocaleProvider';
import { getBrand } from '@/lib/brand-server';
import { siteBrand } from '@/lib/brands';
import { getRequestPathname } from '@/lib/request-pathname';
import { generateMetadata as generatePageMetadata } from './metadata';
import { Metadata } from 'next';
import { Suspense } from 'react';
import { Inter, JetBrains_Mono } from "next/font/google";
import Script from 'next/script';
import ChatwootWidget from '@/components/ChatwootWidget';
import "../globals.css";

// Inline polyfill for Array.prototype.at — Sentry web-vitals (INP) and Next.js
// server-action digest parsing call it. Native on iOS Safari 15.4+ / Chrome 92+.
// Below that (iOS 13.4–15.3) we substitute a spec-faithful implementation so
// those code paths don't TypeError. Sized for inline; ~250 bytes minified.
const ARRAY_AT_POLYFILL = `(function(){if(typeof Array.prototype.at==="function")return;var at=function(n){n=Math.trunc(n)||0;if(n<0)n+=this.length;if(n<0||n>=this.length)return undefined;return this[n]};Object.defineProperty(Array.prototype,"at",{value:at,writable:true,configurable:true});})();`;

const inter = Inter({ subsets: ["latin"] });

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
});

export function generateStaticParams() {
  return siteBrand().allowedLocales.map((locale) => ({ locale }));
}

export async function generateMetadata({
  params
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const brand = await getBrand(locale);
  const pathname = await getRequestPathname();
  return generatePageMetadata(locale, pathname, {}, brand);
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

  // Off-brand locales are 301'd by middleware; notFound() here is the safety
  // net for direct RSC requests that bypass it.
  if (!(siteBrand().allowedLocales as readonly string[]).includes(locale)) {
    notFound();
  }

  // Enable static rendering
  setRequestLocale(locale as (typeof routing.locales)[number]);

  // Providing all messages to the client
  // side is the easiest way to get started
  const messages = await getMessages();
  const brand = await getBrand(locale);

  return (
    <html lang={locale} data-brand={brand.id} suppressHydrationWarning>
      <body className={`${inter.className} ${jetbrainsMono.variable}`} suppressHydrationWarning>
        {/* Inline-rendered <script> JSX inside a React 19 / Next.js 15 server
            component is serialized into the RSC payload, not emitted as a
            synchronous inline <script> tag — verified by inspecting the
            prerendered HTML. The closest reliable approximation is next/script
            with afterInteractive: it runs once hydration completes, which is
            still well before any of our Array.prototype.at call sites
            (Sentry web-vitals INP — needs first user interaction; Next.js
            server-action digest parsing — needs a failed action). */}
        <Script id="array-at-polyfill" strategy="afterInteractive">
          {ARRAY_AT_POLYFILL}
        </Script>
        {/* Must run before hydration: in embed mode (app webview iframe) every
            link click is forwarded to the parent app instead of navigating the
            iframe. See public/embed-interceptor.js. */}
        <Script src="/embed-interceptor.js" strategy="beforeInteractive" />
        {brand.gaMeasurementId && (
          <>
            <Script
              src={`https://www.googletagmanager.com/gtag/js?id=${brand.gaMeasurementId}`}
              strategy="afterInteractive"
            />
            <Script id="google-analytics" strategy="afterInteractive">
              {`
                window.dataLayer = window.dataLayer || [];
                function gtag(){dataLayer.push(arguments);}
                gtag('js', new Date());
                gtag('config', '${brand.gaMeasurementId}');
              `}
            </Script>
          </>
        )}
        <NextIntlClientProvider messages={messages}>
          <LocaleProvider locale={locale}>
            <Suspense fallback={null}>
              <EmbedThemeProvider>
                <AppConfigProvider>
                  <AuthProvider>
                    <BrowserWarningBar brandDomain={new URL(brand.baseUrl).hostname} />
                    <LanguageDetectionBanner />
                    {children}
                    <Toaster />
                    <CookieConsent />
                    <ChatwootWidget />
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