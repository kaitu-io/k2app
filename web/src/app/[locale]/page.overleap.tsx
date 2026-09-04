import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import OverleapHero from '@/components/home-overleap/OverleapHero';
import OverleapSteps from '@/components/home-overleap/OverleapSteps';
import OverleapFeatures from '@/components/home-overleap/OverleapFeatures';
import OverleapPricing from '@/components/home-overleap/OverleapPricing';
import OverleapFAQ from '@/components/home-overleap/OverleapFAQ';
import OverleapDownload from '@/components/home-overleap/OverleapDownload';
import { generateMetadata as generateBaseMetadata } from './metadata';

type Locale = (typeof routing.locales)[number];

const FAQ_KEYS = [
  'whatIsK2', 'lossySpeed', 'ech', 'platforms', 'logs', 'selfHost',
  'pricing', 'payment', 'devices', 'wifiSwitch', 'cancel',
] as const;
const FEATURE_KEYS = ['ech', 'k2cc', 'transport', 'privacy'] as const;
const STEP_KEYS = ['subscribe', 'download', 'connect'] as const;

// Overleap 构建专属首页（page.overleap.tsx）。开途首页在 page.kaitu.tsx；两者由
// next.config 的 pageExtensions 按品牌择一编译，互不可见。
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'landing' });
  const vars = { brand: brand.displayName };
  const title = `${t('landing.meta.title', vars)} | ${brand.displayName}`;
  const description = t('landing.meta.description', vars);
  const base = generateBaseMetadata(locale, '', { title, description }, brand);
  return {
    ...base,
    title,
    description,
    openGraph: { ...(base.openGraph as Record<string, unknown>), title, description },
    twitter: { ...(base.twitter as Record<string, unknown>), title, description },
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
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'landing' });
  const vars = { brand: brand.displayName };

  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: t(`landing.faq.items.${key}.question`, vars),
    answer: t(`landing.faq.items.${key}.answer`, vars),
  }));

  const softwareApplicationJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: brand.displayName,
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android',
    description: t('landing.meta.description', vars),
    url: brand.baseUrl,
    publisher: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
    offers: [
      { '@type': 'Offer', price: '79', priceCurrency: 'USD', name: t('landing.pricing.yearly.name') },
      { '@type': 'Offer', price: '8.99', priceCurrency: 'USD', name: t('landing.pricing.monthly.name') },
    ],
  };
  const organizationJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: brand.displayName,
    url: brand.baseUrl,
    logo: `${brand.baseUrl}${brand.logoPath}`,
    sameAs: ['https://github.com/getoverleap'],
    contactPoint: { '@type': 'ContactPoint', email: brand.contactEmail, contactType: 'customer support' },
  };
  const faqPageJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(softwareApplicationJsonLd) }} />
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(organizationJsonLd) }} />
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(faqPageJsonLd) }} />
      <Header />
      <OverleapHero
        badge={t('landing.hero.badge')}
        title={t('landing.hero.title')}
        subtitle={t('landing.hero.subtitle')}
        description={t('landing.hero.description', vars)}
        ctaPrimary={t('landing.hero.ctaPrimary', vars)}
        ctaSecondary={t('landing.hero.ctaSecondary')}
        mockConnected={t('landing.hero.mockConnected')}
        mockNode={t('landing.hero.mockNode')}
        brandName={brand.displayName}
      />
      <OverleapSteps
        title={t('landing.steps.title')}
        steps={STEP_KEYS.map((key, i) => ({
          key,
          number: String(i + 1).padStart(2, '0'),
          label: t(`landing.steps.${key}.label`),
          detail: t(`landing.steps.${key}.detail`),
        }))}
        cta={t('landing.steps.cta')}
      />
      <OverleapFeatures
        title={t('landing.features.title', vars)}
        features={FEATURE_KEYS.map((key) => ({
          key,
          title: t(`landing.features.${key}.title`, vars),
          description: t(`landing.features.${key}.description`, vars),
        }))}
      />
      <OverleapPricing
        title={t('landing.pricing.title')}
        subtitle={t('landing.pricing.subtitle')}
        plans={[
          { key: 'yearly', featured: true, name: t('landing.pricing.yearly.name'), price: t('landing.pricing.yearly.price'), period: t('landing.pricing.yearly.period'), note: t('landing.pricing.yearly.note') },
          { key: 'monthly', featured: false, name: t('landing.pricing.monthly.name'), price: t('landing.pricing.monthly.price'), period: t('landing.pricing.monthly.period'), note: t('landing.pricing.monthly.note') },
        ]}
        includes={t.raw('landing.pricing.includes') as string[]}
        cta={t('landing.pricing.cta')}
      />
      <OverleapFAQ title={t('landing.faq.title')} subtitle={t('landing.faq.subtitle', vars)} items={faqItems} />
      <OverleapDownload
        title={t('landing.download.title', vars)}
        subtitle={t('landing.download.subtitle')}
        platforms={t('landing.download.platforms')}
        button={t('landing.download.button')}
      />
      <Footer />
    </div>
  );
}
