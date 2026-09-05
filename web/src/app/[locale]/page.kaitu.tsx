import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import HeroSection from '@/components/home/HeroSection';
import FeaturesSection from '@/components/home/FeaturesSection';
import TestimonialsSection from '@/components/home/TestimonialsSection';
import OnboardingSection from '@/components/home/OnboardingSection';
import FAQSection from '@/components/home/FAQSection';
import DownloadCTA from '@/components/home/DownloadCTA';
import HomeClient from './HomeClient';
import { generateMetadata as generateBaseMetadata } from './metadata';
import { getBrand } from '@/lib/brand-server';

type Locale = (typeof routing.locales)[number];

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
  const brand = await getBrand();
  const base = generateBaseMetadata(locale, '', {}, brand);
  const t = await getTranslations({ locale, namespace: 'hero' });

  const title = `${t('hero.title')} | ${brand.wordmark} k2cc`;
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

  const brand = await getBrand();
  const t = await getTranslations({ locale, namespace: 'hero' });

  const FAQ_ITEMS = [
    'whatIsK2cc',
    'whatIsK2v5',
    'howDoesEchWork',
    'networkThrottlingSpeed',
    'platforms',
    'selfHosting',
    'routerSupport',
    'chinaAccess',
    'chinaAppStore',
    'pricing',
    'trial',
    'refund',
    'deviceLimit',
    'privacy',
    'ctLog',
    'portReuse',
    'wifiSwitch',
    'androidInstall',
  ] as const;

  const faqItems = FAQ_ITEMS.map((key) => ({
    key,
    question: t(`faq.items.${key}.question`),
    answer: t(`faq.items.${key}.answer`),
  }));

  const TESTIMONIAL_KEYS = ['item1', 'item2', 'item3'] as const;
  const testimonials = TESTIMONIAL_KEYS.map((key) => ({
    key,
    quote: t(`hero.testimonials.${key}.quote`),
    author: t(`hero.testimonials.${key}.author`),
    tag: t(`hero.testimonials.${key}.tag`),
  }));

  const onboardingSteps = [
    { key: 'step1', number: '01', label: t('hero.onboarding.step1.label'), detail: t('hero.onboarding.step1.detail') },
    { key: 'step2', number: '02', label: t('hero.onboarding.step2.label'), detail: t('hero.onboarding.step2.detail') },
    { key: 'step3', number: '03', label: t('hero.onboarding.step3.label'), detail: t('hero.onboarding.step3.detail') },
  ];

  // Each schema.org entity is emitted as its own <script> tag.
  // Reason: some third-party JSON-LD parsers (browser extensions, AI summarizers)
  // assume the root of an ld+json blob is a single object and crash on a root array.
  const softwareApplicationJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: `${brand.displayName} k2cc`,
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android, Linux',
    description:
      'ECH-based stealth tunnel protocol powered by k2cc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
    url: brand.baseUrl,
    publisher: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
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
  };

  const organizationJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: brand.displayName,
    url: brand.baseUrl,
    logo: `${brand.baseUrl}${brand.logoPath}`,
    sameAs: ['https://github.com/getoverleap'],
    contactPoint: {
      '@type': 'ContactPoint',
      email: brand.contactEmail,
      contactType: 'customer support',
    },
  };

  const faqPageJsonLd = {
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
  };

  return (
    <div className="min-h-screen text-foreground" style={{ backgroundColor: '#050508' }}>
      {/* JSON-LD structured data — content from i18n translations (trusted, server-side) */}
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON.stringify(softwareApplicationJsonLd) }}
      />
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON.stringify(organizationJsonLd) }}
      />
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqPageJsonLd) }}
      />
      <Header />
      <HomeClient />

      <HeroSection
        badge={t('hero.badge')}
        title={t('hero.title')}
        subtitle={t('hero.subtitle')}
        description={t('hero.description')}
        ctaPrimary={t('hero.cta_primary')}
        ctaSecondary={t('hero.cta_secondary')}
        connected={t('hero.connected')}
        nodeInfo={t('hero.nodeInfo')}
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

      <OnboardingSection
        title={t('hero.onboarding.title')}
        steps={onboardingSteps}
        ctaText={t('hero.cta_primary')}
      />

      <TestimonialsSection
        sectionTitle={t('hero.testimonials.title')}
        testimonials={testimonials}
      />

      <FAQSection
        sectionTitle={t('faq.title')}
        sectionSubtitle={t('faq.subtitle')}
        items={faqItems}
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
