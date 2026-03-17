import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import HeroSection from '@/components/home/HeroSection';
import FeaturesSection from '@/components/home/FeaturesSection';
import DownloadCTA from '@/components/home/DownloadCTA';
import HomeClient from './HomeClient';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

/**
 * Generate metadata for the homepage (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'hero' });

  return {
    title: `${t('hero.title')} Kaitu k2`,
    description: t('hero.description'),
  };
}

/**
 * Static JSON-LD structured data for the k2 stealth tunnel application.
 * Content is a hardcoded constant — not user input — so dangerouslySetInnerHTML is safe here.
 */
const JSON_LD_CONTENT = JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'SoftwareApplication',
  name: 'Kaitu k2',
  applicationCategory: 'NetworkingApplication',
  operatingSystem: 'Windows, macOS, iOS, Android, Linux',
  description:
    'ECH-based stealth tunnel protocol powered by k2cc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
  url: 'https://kaitu.io',
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
  featureList: [
    'ECH (Encrypted Client Hello) stealth',
    'QUIC + TCP-WebSocket dual-stack transport',
    'k2cc adaptive rate control',
    'Reverse proxy camouflage',
    'Self-signed certificate + certificate pinning',
    'Zero CT log exposure',
    'One-command deployment',
  ],
});

export default async function Home({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'hero' });

  return (
    <div className="min-h-screen text-foreground" style={{ backgroundColor: '#050508' }}>
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON_LD_CONTENT }}
      />
      <Header />
      <HomeClient />

      <HeroSection
        title={t('hero.title')}
        subtitle={t('hero.subtitle')}
        description={t('hero.description')}
        ctaPrimary={t('hero.cta_primary')}
        ctaSecondary={t('hero.cta_secondary')}
        terminalTitle={t('hero.terminalTitle')}
      />

      <FeaturesSection
        sectionTitle={t('hero.features.title')}
        features={{
          congestion: {
            title: t('hero.features.congestion.title'),
            description: t('hero.features.congestion.description'),
          },
          ech: {
            title: t('hero.features.ech.title'),
            description: t('hero.features.ech.description'),
          },
          transport: {
            title: t('hero.features.transport.title'),
            description: t('hero.features.transport.description'),
          },
          zeroDeploy: {
            title: t('hero.features.zeroDeploy.title'),
            description: t('hero.features.zeroDeploy.description'),
          },
          reverseProxy: {
            title: t('hero.features.reverseProxy.title'),
            description: t('hero.features.reverseProxy.description'),
          },
          selfSign: {
            title: t('hero.features.selfSign.title'),
            description: t('hero.features.selfSign.description'),
          },
        }}
      />

      <DownloadCTA
        title={t('hero.downloadCta.title')}
        subtitle={t('hero.downloadCta.subtitle')}
        buttonText={t('hero.downloadCta.button')}
        platforms={t('hero.downloadCta.platforms')}
      />

      <Footer />
    </div>
  );
}
