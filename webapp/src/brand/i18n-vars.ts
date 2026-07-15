/**
 * Brand ↔ i18n glue. Locale files are brand-neutral and reference
 * {{brand}} / {{brandDomain}} / {{brandBaseUrl}} / {{supportEmail}} via
 * i18next interpolation defaultVariables (installed in i18n/i18n.ts).
 */
import { brandConfig } from './index';

/** Locale-aware brand display name. 中文语境禁用裸词 Kaitu (kaitu → 开途/開途). */
export function getBrandName(locale: string): string {
  const l = locale.toLowerCase();
  const { names } = brandConfig;
  if (l.startsWith('zh')) {
    // zh-TW / zh-HK / zh-Hant* are traditional; every other zh-* is simplified.
    const isHant = l.startsWith('zh-tw') || l.startsWith('zh-hk') || l.startsWith('zh-hant') || l.startsWith('zh-mo');
    if (isHant && names.zhHant) return names.zhHant;
    if (names.zhHans) return names.zhHans;
    if (names.zhHant) return names.zhHant;
  }
  return names.default;
}

export function getBrandSlogan(locale: string): string {
  const slogans = brandConfig.slogans as Record<string, string>;
  return slogans[locale] ?? brandConfig.slogans.default;
}

export function brandI18nVariables(locale: string): Record<string, string> {
  return {
    brand: getBrandName(locale),
    brandDomain: brandConfig.domainLabel,
    brandBaseUrl: brandConfig.baseURL,
    supportEmail: brandConfig.supportEmail,
  };
}
