/**
 * 金额格式。金额一律以最小货币单位（分）传入，与 API `Plan.price` 同量纲。
 */

/**
 * 最小单位金额 → 本地化货币字符串。整数金额不带小数（£79），否则两位（£9.99）；
 * `digits` 可强制。narrowSymbol 让美元在任何 locale 下都是 "$"（币种由旁边的说明文字点明）。
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
    // narrowSymbol：任何 locale 下美元都是 "$"、英镑 "£"（默认 symbol 会在 en-GB / zh 下把美元写成 "US$"）。
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(major);
}
