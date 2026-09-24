import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { landingPrices } from '@/lib/landing-prices';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import OverleapPricing from '@/components/home-overleap/OverleapPricing';
import OverleapFAQ from '@/components/home-overleap/OverleapFAQ';
import { generateMetadata as generateBaseMetadata } from '../metadata';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

// 只放与钱有关的四条（首页 FAQ 的子集）。
const FAQ_KEYS = ['pricing', 'payment', 'cancel', 'devices'] as const;

function pageVars(locale: Locale) {
  const brand = siteBrand();
  const prices = landingPrices(locale);
  return {
    brand: brand.displayName,
    yearly: prices?.yearly ?? '',
    monthly: prices?.monthly ?? '',
    currency: prices?.currency.toUpperCase() ?? '',
  };
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'pricing' });
  const vars = pageVars(locale);
  return generateBaseMetadata(
    locale,
    '/pricing',
    { title: `${t('pricing.metaTitle')} | ${brand.wordmark}`, description: t('pricing.metaDescription', vars) },
    brand,
  );
}

// overleap 构建专属定价页（page.overleap.tsx）：复用首页价表组件与 landing 文案，
// 价格来自 lib/site 的静态价表（tests/pricing-source.test.ts 锁它与 Stripe 建价脚本同源）。
export default async function PricingPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'pricing' });
  const tl = await getTranslations({ locale, namespace: 'landing' });
  const prices = landingPrices(locale);
  const vars = pageVars(locale);

  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: tl(`landing.faq.items.${key}.question`, vars),
    answer: tl(`landing.faq.items.${key}.answer`, vars),
  }));
  const faqPageJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };
  const offersJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'Product',
    name: brand.displayName,
    brand: { '@type': 'Brand', name: brand.displayName },
    offers: (prices?.offers ?? []).map((o) => ({
      '@type': 'Offer',
      price: o.price,
      priceCurrency: o.currency,
      name: tl(`landing.pricing.${o.plan}.name`),
      url: `${brand.baseUrl}/${locale}/purchase`,
    })),
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(faqPageJsonLd).replace(/</g, '\\u003c') }} />
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(offersJsonLd).replace(/</g, '\\u003c') }} />
      <Header />
      <section className="pt-16 pb-4 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <h1 className="text-4xl sm:text-5xl font-bold mb-4">{t('pricing.title')}</h1>
          <p className="text-lg text-muted-foreground">{t('pricing.subtitle')}</p>
        </div>
      </section>
      {prices && (
        <OverleapPricing
          title={tl('landing.pricing.title')}
          subtitle={tl('landing.pricing.subtitle')}
          plans={[
            { key: 'yearly', featured: true, name: tl('landing.pricing.yearly.name'), price: prices.yearly, period: tl('landing.pricing.yearly.period'), note: tl('landing.pricing.yearly.note', { monthly: prices.yearlyPerMonth }) },
            { key: 'monthly', featured: false, name: tl('landing.pricing.monthly.name'), price: prices.monthly, period: tl('landing.pricing.monthly.period'), note: tl('landing.pricing.monthly.note') },
          ]}
          includes={tl.raw('landing.pricing.includes') as string[]}
          cta={tl('landing.pricing.cta')}
          currencyNote={tl('landing.pricing.currencyNote', vars)}
        />
      )}
      <OverleapFAQ title={t('pricing.faqTitle')} subtitle={t('pricing.faqSubtitle')} items={faqItems} />
      <Footer />
    </div>
  );
}
