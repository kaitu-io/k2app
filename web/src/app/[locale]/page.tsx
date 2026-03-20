import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import HeroSection from '@/components/home/HeroSection';
import FeaturesSection from '@/components/home/FeaturesSection';
import DownloadCTA from '@/components/home/DownloadCTA';
import HomeClient from './HomeClient';
import { generateMetadata as generateBaseMetadata } from './metadata';

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
  const base = generateBaseMetadata(locale);
  const t = await getTranslations({ locale, namespace: 'hero' });

  const title = `${t('hero.title')} | Kaitu k2cc`;
  const description = t('hero.description');

  return {
    ...base,
    title,
    description,
    openGraph: {
      ...(base.openGraph as Record<string, unknown>),
      title,
      description,
    },
    twitter: {
      ...(base.twitter as Record<string, unknown>),
      title,
      description,
    },
  };
}

export default async function Home({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'hero' });

  const FAQ_ITEMS = ['platforms', 'pricing', 'refund', 'androidInstall', 'privacy'] as const;

  const jsonLd = JSON.stringify([
    {
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      name: 'Kaitu k2cc',
      applicationCategory: 'NetworkingApplication',
      operatingSystem: 'Windows, macOS, iOS, Android, Linux',
      description:
        'ECH-based stealth tunnel protocol powered by k2cc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
      url: 'https://kaitu.io',
      publisher: { '@type': 'Organization', name: 'Kaitu', url: 'https://kaitu.io' },
      offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
      featureList: [
        'ECH (Encrypted Client Hello) stealth',
        'QUIC + TCP-WebSocket dual-stack transport',
        'k2cc adaptive rate control',
        'Reverse proxy camouflage',
        'Self-signed certificate + certificate pinning',
        'Zero CT log exposure',
        'One-command deployment',
      ],
    },
    {
      '@context': 'https://schema.org',
      '@type': 'Organization',
      name: 'Kaitu',
      url: 'https://kaitu.io',
      logo: 'https://kaitu.io/kaitu-icon.png',
      sameAs: ['https://github.com/kaitu-io'],
      contactPoint: {
        '@type': 'ContactPoint',
        email: 'support@kaitu.io',
        contactType: 'customer support',
      },
    },
    {
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: FAQ_ITEMS.map((item) => ({
        '@type': 'Question',
        name: t(`faq.items.${item}.question`),
        acceptedAnswer: {
          '@type': 'Answer',
          text: t(`faq.items.${item}.answer`),
        },
      })),
    },
  ]);

  return (
    <div className="min-h-screen text-foreground" style={{ backgroundColor: '#050508' }}>
      {/* JSON-LD structured data — content from i18n translations (trusted, server-side) */}
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: jsonLd }}
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
