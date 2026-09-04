import { describe, expect, it } from 'vitest';
import { displayCurrency, formatMinor, planAmount } from '../pricing';

describe('utils/pricing', () => {
  it('en-GB shows pounds, everything else dollars', () => {
    expect(displayCurrency('en-GB')).toBe('gbp');
    expect(displayCurrency('en-US')).toBe('usd');
    expect(displayCurrency('ja')).toBe('usd');
    expect(displayCurrency('zh-CN')).toBe('usd');
  });

  it('planAmount prefers the display currency, then usd, then Plan.price', () => {
    const plan = { price: 7900, currencyPrices: { usd: 7900, gbp: 7900, eur: 8900 } };
    expect(planAmount(plan, 'en-GB')).toEqual({ amount: 7900, currency: 'gbp' });
    expect(planAmount(plan, 'en-US')).toEqual({ amount: 7900, currency: 'usd' });
    expect(planAmount({ price: 7900, currencyPrices: { eur: 8900, usd: 7900 } }, 'en-GB')).toEqual({ amount: 7900, currency: 'usd' });
    expect(planAmount({ price: 7900 }, 'en-GB')).toEqual({ amount: 7900, currency: 'usd' });
  });

  it('formats whole amounts without decimals and fractional ones with two', () => {
    expect(formatMinor(7900, 'gbp', 'en-GB')).toBe('£79');
    expect(formatMinor(999, 'gbp', 'en-GB')).toBe('£9.99');
    expect(formatMinor(1199, 'usd', 'en-US')).toBe('$11.99');
    expect(formatMinor(7900 / 12, 'gbp', 'en-GB', 2)).toBe('£6.58');
  });
});
