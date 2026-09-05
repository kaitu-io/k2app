import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import OverleapInstall from '@/components/install-overleap/OverleapInstall';
import { fetchAllDownloadLinks } from '@/lib/downloads';
import { buildInstallTargets } from '@/lib/install-targets';
import { siteBrand } from '@/lib/brands';
import { generateMetadata as generateBaseMetadata } from '../metadata';

type Locale = (typeof routing.locales)[number];

export const revalidate = 300; // 5 min ISR — fetch latest version from CDN manifests

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'download' });
  const vars = { brand: brand.displayName };
  return generateBaseMetadata(locale, '/install', {
    title: `${t('download.meta.title', vars)} | ${brand.displayName}`,
    description: t('download.meta.description', vars),
  }, brand);
}

// overleap 构建专属下载页（page.overleap.tsx）。kaitu 的安装页在 page.kaitu.tsx。
export default async function InstallPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const brand = siteBrand();
  const all = await fetchAllDownloadLinks();
  const targets = buildInstallTargets(all, brand);
  const desktopVersion = (all.desktop.stable ?? all.desktop.beta)?.version;

  const jsonLd = {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: brand.displayName,
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android',
    softwareVersion: desktopVersion,
    downloadUrl: `${brand.baseUrl}/install`,
    url: `${brand.baseUrl}/install`,
    publisher: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
  };

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <OverleapInstall targets={targets} />
      <Footer />
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />
    </div>
  );
}
