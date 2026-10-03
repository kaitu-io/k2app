import { getTranslations } from 'next-intl/server';
import { landingPrices } from '@/lib/pricing';
import type { Locale } from '@/lib/site';

/** Home + /pricing share the static price card block. */
export async function landingPricing(locale: Locale) {
  const t = await getTranslations({ locale, namespace: 'landing' });
  const prices = landingPrices(locale);
  const currency = prices.currency.toUpperCase();
  return {
    prices,
    props: {
      title: t('pricing.title'),
      subtitle: t('pricing.subtitle'),
      plans: [
        { key: 'yearly' as const, featured: true, name: t('pricing.yearly.name'), price: prices.yearly, period: t('pricing.yearly.period'), note: t('pricing.yearly.note', { monthly: prices.yearlyPerMonth }) },
        { key: 'monthly' as const, featured: false, name: t('pricing.monthly.name'), price: prices.monthly, period: t('pricing.monthly.period'), note: t('pricing.monthly.note') },
      ],
      includes: t.raw('pricing.includes') as string[],
      cta: t('pricing.cta'),
      currencyNote: t('pricing.currencyNote', { currency }),
    },
  };
}
