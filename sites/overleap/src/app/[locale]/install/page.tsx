import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import JsonLd from '@/components/marketing/JsonLd';
import InstallCards from './InstallCards';
import { buildInstallTargets, fetchAllDownloadLinks } from '@/lib/downloads';
import { pageMetadata } from '@/lib/metadata';
import { SITE } from '@/lib/site';

// 5-minute ISR: the latest versions come from the CDN manifests.
export const revalidate = 300;

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'download' });
  const vars = { brand: SITE.name };
  return pageMetadata(locale, '/install', {
    title: `${t('meta.title', vars)} | ${SITE.name}`,
    description: t('meta.description', vars),
  });
}

export default async function InstallPage({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);

  const all = await fetchAllDownloadLinks();
  const targets = buildInstallTargets(all);
  const desktopVersion = (all.desktop.stable ?? all.desktop.beta)?.version;

  const softwareApplication = {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: SITE.name,
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android',
    softwareVersion: desktopVersion,
    downloadUrl: `${SITE.baseUrl}/install`,
    url: `${SITE.baseUrl}/install`,
    publisher: { '@type': 'Organization', name: SITE.name, url: SITE.baseUrl },
  };

  return (
    <SiteShell locale={locale}>
      <InstallCards targets={targets} />
      <JsonLd data={softwareApplication} />
    </SiteShell>
  );
}
