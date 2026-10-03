import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import PricingPlans from '@/components/marketing/PricingPlans';
import Faq from '@/components/marketing/Faq';
import JsonLd from '@/components/marketing/JsonLd';
import { landingPricing } from '@/components/marketing/landing-pricing';
import { pageMetadata } from '@/lib/metadata';
import { priceVars } from '@/lib/pricing';
import { SITE } from '@/lib/site';
import ChatWidgetLazy from '@/components/chat/ChatWidgetLazy';

export const dynamic = 'force-static';

// The money questions of the home FAQ.
const FAQ_KEYS = ['pricing', 'payment', 'cancel', 'devices'] as const;

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'pricing' });
  return pageMetadata(locale, '/pricing', {
    title: `${t('metaTitle')} | ${SITE.name}`,
    description: t('metaDescription', { brand: SITE.name, ...priceVars(locale) }),
  });
}

export default async function PricingPage({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'pricing' });
  const tl = await getTranslations({ locale, namespace: 'landing' });
  const { prices, props: pricingProps } = await landingPricing(locale);
  const vars = { brand: SITE.name, ...priceVars(locale) };

  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: tl(`faq.items.${key}.question`),
    answer: tl(`faq.items.${key}.answer`, vars),
  }));
  const faqPage = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };
  const product = {
    '@context': 'https://schema.org',
    '@type': 'Product',
    name: SITE.name,
    brand: { '@type': 'Brand', name: SITE.name },
    offers: prices.offers.map((o) => ({
      '@type': 'Offer',
      price: o.price,
      priceCurrency: o.currency,
      name: tl(`pricing.${o.plan}.name`),
      url: `${SITE.baseUrl}/${locale}/purchase`,
    })),
  };

  return (
    <SiteShell locale={locale}>
      <JsonLd data={faqPage} />
      <JsonLd data={product} />
      <section className="px-4 pb-4 pt-16 sm:px-6 lg:px-8">
        <div className="mx-auto max-w-4xl text-center">
          <h1 className="mb-4 text-4xl font-bold sm:text-5xl">{t('title')}</h1>
          <p className="text-lg text-muted-foreground">{t('subtitle')}</p>
        </div>
      </section>
      <PricingPlans {...pricingProps} />
      <Faq title={t('faqTitle')} subtitle={t('faqSubtitle')} items={faqItems} />
      <ChatWidgetLazy />
    </SiteShell>
  );
}
