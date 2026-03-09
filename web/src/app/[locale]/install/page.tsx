import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { Card } from '@/components/ui/card';
import InstallClient from '@/app/[locale]/install/InstallClient';
import { CDN_PRIMARY, CDN_BACKUP } from '@/lib/constants';

type Locale = (typeof routing.locales)[number];

export const revalidate = 300; // 5 min ISR — fetch latest version from CDN manifests

async function fetchDesktopVersion(channel: 'beta' | 'stable'): Promise<string | null> {
  const path = channel === 'beta' ? '/beta/cloudfront.latest.json' : '/cloudfront.latest.json';
  for (const base of [CDN_PRIMARY, CDN_BACKUP]) {
    try {
      const res = await fetch(`${base}${path}`, { next: { revalidate: 300 } });
      if (res.ok) {
        const data = await res.json();
        if (data.version) return data.version;
      }
    } catch {}
  }
  return null;
}

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
  const t = await getTranslations({ locale, namespace: 'install' });

  const [betaVersion, stableVersion] = await Promise.all([
    fetchDesktopVersion('beta'),
    fetchDesktopVersion('stable'),
  ]);

  return (
    <div className="min-h-screen bg-background">
      <Header />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <h1 className="text-3xl sm:text-4xl font-bold text-foreground text-center mb-2">
          {t('install.title')}
        </h1>
        <p className="text-muted-foreground text-center mb-8">
          {t('install.allDownloadOptions')}
        </p>

        {/* Client island: device detection, download countdown, download state cards */}
        <InstallClient betaVersion={betaVersion} stableVersion={stableVersion} />

        {/* Help Section — static server-rendered content */}
        <Card className="p-6 mt-8 bg-muted">
          <h4 className="font-semibold text-foreground mb-3">
            {t('install.needHelp')}
          </h4>
          <ul className="text-sm text-muted-foreground space-y-2">
            <li>{t('install.helpBrowserBlock')}</li>
            <li>{t('install.helpWindowsInstall')}</li>
            <li>{t('install.helpMacosInstall')}</li>
            <li>{t('install.helpSecurityWarning')}</li>
          </ul>
        </Card>
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
