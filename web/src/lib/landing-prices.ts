import { siteConfig } from '@/lib/site';
import { displayCurrency, formatMinor, monthlyEquivalent } from '@/lib/pricing';

export interface LandingPrices {
  currency: string;
  yearly: string;
  monthly: string;
  yearlyPerMonth: string;
  offers: Array<{ plan: 'yearly' | 'monthly'; currency: string; price: string }>;
}

/** 首页 / 定价页的静态价表（lib/site，与 Stripe 建价脚本同源）按 locale 取展示币。没有价表的品牌返回 null。 */
export function landingPrices(locale: string): LandingPrices | null {
  const pricing = siteConfig().pricing;
  if (!pricing) return null;
  const currency = displayCurrency(locale);
  const yearly = pricing.yearly[currency] ?? pricing.yearly.usd;
  const monthly = pricing.monthly[currency] ?? pricing.monthly.usd;
  const cur = pricing.yearly[currency] === undefined ? 'usd' : currency;
  return {
    currency: cur,
    yearly: formatMinor(yearly, cur, locale),
    monthly: formatMinor(monthly, cur, locale),
    yearlyPerMonth: formatMinor(monthlyEquivalent(yearly), cur, locale, { digits: 2 }),
    offers: (['yearly', 'monthly'] as const).flatMap((plan) =>
      Object.entries(pricing[plan]).map(([c, minor]) => ({ plan, currency: c.toUpperCase(), price: (minor / 100).toFixed(2) })),
    ),
  };
}
