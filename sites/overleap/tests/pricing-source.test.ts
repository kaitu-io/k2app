/**
 * One source for prices. Stripe Prices are created by the `ensure_price` lines of
 * scripts/stripe-setup-overleap.sh (the only way they are made, idempotent); the
 * home and pricing pages show the static table in lib/site.ts. The two must agree
 * currency by currency, or the site advertises a price Checkout does not charge.
 * The purchase page reads live `currencyPrices` from the API and is not compared here.
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { LOCALES, PRICING } from '@/lib/site';
import { displayCurrency, formatMinor, landingPrices, monthlyEquivalent, pickAmount } from '@/lib/pricing';

const SCRIPT = path.resolve(__dirname, '../../../scripts/stripe-setup-overleap.sh');

/** `ensure_price  overleap_basic_1y year 7900 7900 8900` → { usd, gbp, eur } */
function scriptPrices(): Record<string, { usd: number; gbp: number; eur: number }> {
  const src = fs.readFileSync(SCRIPT, 'utf8');
  const out: Record<string, { usd: number; gbp: number; eur: number }> = {};
  for (const m of src.matchAll(/^ensure_price\s+(\S+)\s+(year|month)\s+(\d+)\s+(\d+)\s+(\d+)\s*$/gm)) {
    out[m[2]] = { usd: Number(m[3]), gbp: Number(m[4]), eur: Number(m[5]) };
  }
  return out;
}

describe('pricing has one source', () => {
  const fromScript = scriptPrices();

  it('the Stripe setup script declares a yearly and a monthly price', () => {
    expect(Object.keys(fromScript).sort()).toEqual(['month', 'year']);
  });

  it('lib/site.ts PRICING equals the script, currency by currency', () => {
    expect(PRICING.yearly).toEqual(fromScript.year);
    expect(PRICING.monthly).toEqual(fromScript.month);
  });

  it('plan ids match the script lookup keys', () => {
    const keys = [...fs.readFileSync(SCRIPT, 'utf8').matchAll(/^ensure_price\s+(\S+)\s+(year|month)/gm)].map((m) => [m[2], m[1]]);
    const byInterval = Object.fromEntries(keys);
    expect(PRICING.pids.yearly.replace(/-/g, '_')).toBe(byInterval.year);
    expect(PRICING.pids.monthly.replace(/-/g, '_')).toBe(byInterval.month);
  });

  it('the script creates USD-primary prices with GBP / EUR options', () => {
    const src = fs.readFileSync(SCRIPT, 'utf8');
    expect(src).toMatch(/-d currency=usd/);
    expect(src).toMatch(/currency_options\[gbp\]/);
    expect(src).toMatch(/currency_options\[eur\]/);
  });
});

describe('display currency by locale', () => {
  it('en-GB shows pounds, every other locale dollars', () => {
    expect(displayCurrency('en-GB')).toBe('gbp');
    for (const l of LOCALES.filter((l) => l !== 'en-GB')) expect(displayCurrency(l), l).toBe('usd');
  });

  it('formats whole amounts without decimals and fractional ones with two', () => {
    expect(formatMinor(7900, 'gbp', 'en-GB')).toBe('£79');
    expect(formatMinor(999, 'gbp', 'en-GB')).toBe('£9.99');
    expect(formatMinor(7900, 'usd', 'en-US')).toBe('$79');
    expect(formatMinor(1199, 'usd', 'en-US')).toBe('$11.99');
    expect(formatMinor(monthlyEquivalent(7900), 'gbp', 'en-GB', { digits: 2 })).toBe('£6.58');
  });

  // Intl must cope with every served locale (my / km / fa use their own digits and symbol placement).
  it.each(LOCALES)('%s: landing prices render', (locale) => {
    const p = landingPrices(locale);
    for (const s of [p.yearly, p.monthly, p.yearlyPerMonth]) {
      expect(s, `${locale} ${s}`).toMatch(/\p{Nd}/u);
      expect(s, `${locale} ${s}`).toMatch(/[$£]/);
    }
    expect(p.offers).toHaveLength(6);
  });

  it('pickAmount prefers the display currency, then usd, then anything', () => {
    expect(pickAmount({ usd: 7900, gbp: 7900, eur: 8900 }, 'gbp')).toEqual({ amount: 7900, currency: 'gbp' });
    expect(pickAmount({ usd: 7900, eur: 8900 }, 'gbp')).toEqual({ amount: 7900, currency: 'usd' });
    expect(pickAmount({ eur: 8900 }, 'gbp')).toEqual({ amount: 8900, currency: 'eur' });
    expect(pickAmount(undefined, 'gbp')).toBeUndefined();
  });
});
