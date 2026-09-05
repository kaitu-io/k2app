/**
 * 展示币种与金额格式（与 web/src/lib/pricing.ts 同规则）。
 *
 * 实付币种由 Stripe Checkout 按客户属地在 Price 的 currency_options 里自动选；app 只能按
 * 语言给出"你大概会付多少"。金额一律最小货币单位（分 / 便士），与 API `currencyPrices` /
 * `Plan.price` 同量纲。
 */
import type { Plan } from '../services/api-types';

export type DisplayCurrency = 'usd' | 'gbp' | 'eur';

/** en-GB → 英镑；其余 → 美元。 */
export function displayCurrency(locale: string): DisplayCurrency {
  return locale === 'en-GB' ? 'gbp' : 'usd';
}

/** 套餐在该语言下的展示价：优先 API 的 currencyPrices（Stripe 真相），缺席或缺币种回落 usd / price。 */
export function planAmount(plan: Pick<Plan, 'price' | 'currencyPrices'>, locale: string): { amount: number; currency: string } {
  const want = displayCurrency(locale);
  const cp = plan.currencyPrices;
  if (cp) {
    if (typeof cp[want] === 'number') return { amount: cp[want], currency: want };
    if (typeof cp.usd === 'number') return { amount: cp.usd, currency: 'usd' };
  }
  return { amount: plan.price, currency: 'usd' };
}

/** 最小单位 → 本地化货币字符串；整数金额不带小数（£79），否则两位（£9.99），`digits` 可强制。 */
export function formatMinor(amount: number, currency: string, locale: string, digits?: number): string {
  const major = amount / 100;
  const d = digits ?? (Number.isInteger(major) ? 0 : 2);
  try {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency: currency.toUpperCase(),
      // narrowSymbol：任何 locale 下美元都是 "$"、英镑 "£"（默认 symbol 会在 en-GB / zh 下把美元写成 "US$"）。
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits: d,
      maximumFractionDigits: d,
    }).format(major);
  } catch {
    return `${currency.toUpperCase()} ${major.toFixed(d)}`;
  }
}
