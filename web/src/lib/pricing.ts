/**
 * 展示币种与金额格式（spec 2026-09-04-overleap-site-decoupling §4.4）。
 *
 * 实付币种由 Stripe Checkout 按客户属地在 Price 的 currency_options 里自动选；网站只能按
 * locale 给出"你大概会付多少"，并明示 charged in your local currency where available。
 * 金额一律以最小货币单位（分 / 便士）传入，与 API `currencyPrices` / `Plan.price` 同量纲。
 */

export type DisplayCurrency = 'usd' | 'gbp' | 'eur';

/** 按 locale 选展示币种：英式英语 → 英镑；其余（en-US / en-AU / ja …）→ 美元。
 *  未来加入欧陆 locale 时在这里映射到 'eur'。 */
export function displayCurrency(locale: string): DisplayCurrency {
  if (locale === 'en-GB') return 'gbp';
  return 'usd';
}

/** 从多币种价表里取展示币的金额；缺该币种时回落 usd，再回落任一存在的币种。 */
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
 * 最小单位金额 → 本地化货币字符串。整数金额不带小数（£79），否则两位（£9.99）；
 * `digits` 可强制。en-AU 下美元会显示为 "USD 79"（Intl 的消歧义行为，对澳洲用户是对的）。
 */
export function formatMinor(
  amount: number,
  currency: string,
  locale: string,
  opts: { digits?: number } = {},
): string {
  const major = amount / 100;
  const digits = opts.digits ?? (Number.isInteger(major) ? 0 : 2);
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(major);
}

/** 年付折合月价（最小单位，保留小数以便 formatMinor 两位显示）。 */
export function monthlyEquivalent(yearlyMinor: number): number {
  return yearlyMinor / 12;
}
