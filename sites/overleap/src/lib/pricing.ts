import { PRICING } from './site';

/**
 * Display currency and money formatting.
 *
 * Stripe Checkout picks the charged currency from the customer's location
 * (the Price's currency_options); the site can only show "roughly what you will
 * pay" for the reader's locale and says so ("charged in your local currency
 * where available"). Amounts are always minor units (cents / pence), the same
 * unit as the API's `currencyPrices` / `Plan.price`.
 */

export type DisplayCurrency = 'usd' | 'gbp' | 'eur';

/** British English → pounds; every other locale → dollars. Map a locale to 'eur' here if one ever needs it. */
export function displayCurrency(locale: string): DisplayCurrency {
  if (locale === 'en-GB') return 'gbp';
  return 'usd';
}

/** The display currency's amount from a multi-currency table; falls back to usd, then to any currency present. */
export function pickAmount(
  amounts: Partial<Record<string, number>> | undefined,
  currency: DisplayCurrency,
): { amount: number; currency: string } | undefined {
  if (!amounts) return undefined;
  const direct = amounts[currency];
  if (typeof direct === 'number') return { amount: direct, currency };
  if (typeof amounts.usd === 'number') return { amount: amounts.usd, currency: 'usd' };
  const [cur, amt] = Object.entries(amounts).find(([, v]) => typeof v === 'number') ?? [];
  return cur && typeof amt === 'number' ? { amount: amt, currency: cur } : undefined;
}

/**
 * Minor units → localised currency string. Whole amounts have no decimals (£79),
 * others two (£9.99); `digits` forces either. `narrowSymbol` keeps dollars "$"
 * in every locale (the default `symbol` writes "US$" in en-GB); the currency is
 * named by the note next to the price. Browsers too old for `narrowSymbol`
 * (Safari < 14.1) throw RangeError, so fall back to the default symbol.
 */
export function formatMinor(
  amount: number,
  currency: string,
  locale: string,
  opts: { digits?: number } = {},
): string {
  const major = amount / 100;
  const digits = opts.digits ?? (Number.isInteger(major) ? 0 : 2);
  const base: Intl.NumberFormatOptions = {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  };
  try {
    return new Intl.NumberFormat(locale, { ...base, currencyDisplay: 'narrowSymbol' }).format(major);
  } catch {
    return new Intl.NumberFormat(locale, base).format(major);
  }
}

/** Yearly price spread over twelve months (minor units, fractional kept for two-decimal display). */
export function monthlyEquivalent(yearlyMinor: number): number {
  return yearlyMinor / 12;
}

export interface LandingPrices {
  /** Lower-case currency actually shown. */
  currency: string;
  yearly: string;
  monthly: string;
  yearlyPerMonth: string;
  /** JSON-LD offers: every currency of the static table. */
  offers: Array<{ plan: 'yearly' | 'monthly'; currency: string; price: string }>;
}

/** The static price table (lib/site PRICING, same source as the Stripe setup script) in the locale's display currency. */
export function landingPrices(locale: string): LandingPrices {
  const currency = displayCurrency(locale);
  const yearly = PRICING.yearly[currency];
  const monthly = PRICING.monthly[currency];
  return {
    currency,
    yearly: formatMinor(yearly, currency, locale),
    monthly: formatMinor(monthly, currency, locale),
    yearlyPerMonth: formatMinor(monthlyEquivalent(yearly), currency, locale, { digits: 2 }),
    offers: (['yearly', 'monthly'] as const).flatMap((plan) =>
      Object.entries(PRICING[plan]).map(([c, minor]) => ({ plan, currency: c.toUpperCase(), price: (minor / 100).toFixed(2) })),
    ),
  };
}

/** Interpolation values every pricing-bearing message needs ({yearly} {monthly} {currency}). */
export function priceVars(locale: string): { yearly: string; monthly: string; currency: string } {
  const p = landingPrices(locale);
  return { yearly: p.yearly, monthly: p.monthly, currency: p.currency.toUpperCase() };
}
