import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { Card } from '@/components/ui/card';
import InstallClient from '@/app/[locale]/install/InstallClient';

type Locale = (typeof routing.locales)[number];

export const revalidate = 300; // 5 min ISR — fetch latest version from CDN manifests

async function fetchDesktopVersion(channel: 'beta' | 'stable'): Promise<string | null> {
  const path = channel === 'beta' ? '/beta/cloudfront.latest.json' : '/cloudfront.latest.json';
  try {
    const res = await fetch(
      `https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop${path}`,
      { next: { revalidate: 300 } }
    );
    if (!res.ok) return null;
    const data = await res.json();
    return data.version || null;
  } catch {
    return null;
  }
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
    description: t('install.allDownloadOptions'),
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
    </div>
  );
}
