/**
 * 定价三处同源守卫（spec 2026-09-04-overleap-site-decoupling §4.4）。
 *
 * Stripe Price 由 scripts/stripe-setup-overleap.sh 的 `ensure_price` 行创建（唯一建法，
 * 幂等）；网站首页的静态价表在 lib/site/overleap.ts。两者必须逐币种相等，否则首页宣传的
 * 价格与 Checkout 实收不一致。购买页走 API currencyPrices（Stripe 真相），不在此比对。
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { OVERLEAP_SITE } from '../src/lib/site/overleap';
import { displayCurrency, formatMinor, monthlyEquivalent, pickAmount } from '../src/lib/pricing';

const SCRIPT = path.resolve(__dirname, '../../scripts/stripe-setup-overleap.sh');

/** `ensure_price  overleap_basic_1y year 7900 7900 8900` → { usd, gbp, eur } */
function scriptPrices(): Record<string, { usd: number; gbp: number; eur: number }> {
  const src = fs.readFileSync(SCRIPT, 'utf8');
  const out: Record<string, { usd: number; gbp: number; eur: number }> = {};
  for (const m of src.matchAll(/^ensure_price\s+(\S+)\s+(year|month)\s+(\d+)\s+(\d+)\s+(\d+)\s*$/gm)) {
    out[m[2]] = { usd: Number(m[3]), gbp: Number(m[4]), eur: Number(m[5]) };
  }
  return out;
}

describe('overleap pricing has one source', () => {
  const fromScript = scriptPrices();

  it('the Stripe setup script declares a yearly and a monthly price', () => {
    expect(Object.keys(fromScript).sort()).toEqual(['month', 'year']);
  });

  it('lib/site/overleap.ts pricing equals the script, currency by currency', () => {
    expect(OVERLEAP_SITE.pricing).toBeDefined();
    expect(OVERLEAP_SITE.pricing!.yearly).toEqual(fromScript.year);
    expect(OVERLEAP_SITE.pricing!.monthly).toEqual(fromScript.month);
  });

  it('the script creates USD-primary prices (Stripe account settlement currency)', () => {
    const src = fs.readFileSync(SCRIPT, 'utf8');
    expect(src).toMatch(/-d currency=usd/);
    expect(src).toMatch(/currency_options\[gbp\]/);
    expect(src).toMatch(/currency_options\[eur\]/);
  });
});

describe('display currency by locale', () => {
  it('en-GB shows pounds, everything else dollars', () => {
    expect(displayCurrency('en-GB')).toBe('gbp');
    expect(displayCurrency('en-US')).toBe('usd');
    expect(displayCurrency('en-AU')).toBe('usd');
    expect(displayCurrency('ja')).toBe('usd');
  });

  it('formats whole amounts without decimals and fractional ones with two', () => {
    expect(formatMinor(7900, 'gbp', 'en-GB')).toBe('£79');
    expect(formatMinor(999, 'gbp', 'en-GB')).toBe('£9.99');
    expect(formatMinor(7900, 'usd', 'en-US')).toBe('$79');
    expect(formatMinor(1199, 'usd', 'en-US')).toBe('$11.99');
    expect(formatMinor(monthlyEquivalent(7900), 'gbp', 'en-GB', { digits: 2 })).toBe('£6.58');
  });

  it('pickAmount prefers the display currency, then usd, then anything', () => {
    expect(pickAmount({ usd: 7900, gbp: 7900, eur: 8900 }, 'gbp')).toEqual({ amount: 7900, currency: 'gbp' });
    expect(pickAmount({ usd: 7900, eur: 8900 }, 'gbp')).toEqual({ amount: 7900, currency: 'usd' });
    expect(pickAmount({ eur: 8900 }, 'gbp')).toEqual({ amount: 8900, currency: 'eur' });
    expect(pickAmount(undefined, 'gbp')).toBeUndefined();
  });
});
