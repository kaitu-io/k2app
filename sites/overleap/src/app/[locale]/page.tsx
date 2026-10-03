import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import Hero from '@/components/marketing/Hero';
import Steps from '@/components/marketing/Steps';
import Features, { FEATURE_KEYS } from '@/components/marketing/Features';
import PricingPlans from '@/components/marketing/PricingPlans';
import Faq from '@/components/marketing/Faq';
import DownloadCta from '@/components/marketing/DownloadCta';
import JsonLd from '@/components/marketing/JsonLd';
import { pageMetadata } from '@/lib/metadata';
import { landingPricing } from '@/components/marketing/landing-pricing';
import { SITE } from '@/lib/site';

export const dynamic = 'force-static';

const FAQ_KEYS = [
  'logs', 'isp', 'legal', 'publicWifi', 'travel', 'ech',
  'platforms', 'devices', 'pricing', 'payment', 'cancel',
] as const;
const STEP_KEYS = ['subscribe', 'download', 'connect'] as const;

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'landing' });
  const vars = { brand: SITE.name };
  return pageMetadata(locale, '', {
    title: `${t('meta.title')} | ${SITE.name}`,
    description: t('meta.description', vars),
  });
}

export default async function Home({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'landing' });
  const { prices, props: pricingProps } = await landingPricing(locale);
  const vars = { brand: SITE.name, yearly: prices.yearly, monthly: prices.monthly };

  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: t(`faq.items.${key}.question`),
    answer: t(`faq.items.${key}.answer`, vars),
  }));

  const softwareApplication = {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: SITE.name,
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android',
    description: t('meta.description', vars),
    url: SITE.baseUrl,
    publisher: { '@type': 'Organization', name: SITE.name, url: SITE.baseUrl },
    offers: prices.offers.map((o) => ({
      '@type': 'Offer',
      price: o.price,
      priceCurrency: o.currency,
      name: t(`pricing.${o.plan}.name`),
    })),
  };
  const organization = {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: SITE.name,
    url: SITE.baseUrl,
    logo: `${SITE.baseUrl}${SITE.logoPath}`,
    contactPoint: { '@type': 'ContactPoint', email: SITE.contactEmail, contactType: 'customer support' },
  };
  const faqPage = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };

  return (
    <SiteShell locale={locale}>
      <JsonLd data={softwareApplication} />
      <JsonLd data={organization} />
      <JsonLd data={faqPage} />
      <Hero
        badge={t('hero.badge')}
        title={t('hero.title')}
        subtitle={t('hero.subtitle')}
        description={t('hero.description', vars)}
        ctaPrimary={t('hero.ctaPrimary', vars)}
        ctaSecondary={t('hero.ctaSecondary')}
        mockConnected={t('hero.mockConnected')}
        mockNode={t('hero.mockNode')}
        brandName={SITE.name}
      />
      <Steps
        title={t('steps.title')}
        steps={STEP_KEYS.map((key, i) => ({
          key,
          number: String(i + 1).padStart(2, '0'),
          label: t(`steps.${key}.label`),
          detail: t(`steps.${key}.detail`),
        }))}
        cta={t('steps.cta')}
      />
      <Features
        title={t('features.title', vars)}
        features={FEATURE_KEYS.map((key) => ({
          key,
          title: t(`features.${key}.title`),
          description: t(`features.${key}.description`, vars),
        }))}
      />
      <PricingPlans {...pricingProps} />
      <Faq title={t('faq.title')} subtitle={t('faq.subtitle', vars)} items={faqItems} />
      <DownloadCta
        title={t('download.title', vars)}
        subtitle={t('download.subtitle')}
        platforms={t('download.platforms')}
        button={t('download.button')}
      />
    </SiteShell>
  );
}
