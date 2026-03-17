import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import InstallClient from '@/app/[locale]/install/InstallClient';
import { fetchAllDownloadLinks } from '@/lib/downloads';

type Locale = (typeof routing.locales)[number];

export const revalidate = 300; // 5 min ISR — fetch latest version from CDN manifests

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'install' });
  return {
    title: t('install.title'),
    description: t('install.metaDescription'),
  };
}

export default async function InstallPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const all = await fetchAllDownloadLinks();
  const betaVersion = all.desktop.beta?.version ?? null;
  const stableVersion = all.desktop.stable?.version ?? null;

  return (
    <div className="min-h-screen bg-background">
      <Header />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Client island: device detection, download countdown, download state cards */}
        <InstallClient betaVersion={betaVersion} stableVersion={stableVersion} mobileLinks={all.mobile} />
      </div>

      <Footer />

      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify({
            '@context': 'https://schema.org',
            '@type': 'SoftwareApplication',
            name: 'Kaitu',
            applicationCategory: 'NetworkingApplication',
            operatingSystem: 'Windows, macOS, iOS, Android',
            softwareVersion: stableVersion || betaVersion || undefined,
            downloadUrl: 'https://kaitu.io/install',
            url: 'https://kaitu.io/install',
            publisher: {
              '@type': 'Organization',
              name: 'Kaitu',
              url: 'https://kaitu.io',
            },
            offers: {
              '@type': 'Offer',
              price: '0',
              priceCurrency: 'USD',
            },
          }),
        }}
      />
    </div>
  );
}
