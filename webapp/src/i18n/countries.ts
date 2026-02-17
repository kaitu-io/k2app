import countries from "i18n-iso-countries";
import zhLocale from "i18n-iso-countries/langs/zh.json";
import enLocale from "i18n-iso-countries/langs/en.json";
import jaLocale from "i18n-iso-countries/langs/ja.json";
import i18n, { languages } from "./i18n";

// 获取对应的locale文件
function getLocaleForLanguage(langCode: string) {
  const baseLang = langCode.split('-')[0];
  switch (baseLang) {
    case 'en': return enLocale;
    case 'zh': return zhLocale;
    case 'ja': return jaLocale;
    default: return null;
  }
}

// 注册所有支持的语言（基于i18n配置的languages常量）
Object.keys(languages).forEach(langCode => {
  const locale = getLocaleForLanguage(langCode);
  if (locale) {
    try {
      countries.registerLocale(locale);
    } catch {
      // 忽略重复注册错误
    }
  }
});

// 获取当前i18n语言对应的countries库语言代码
function getCurrentLanguage(): string {
  const currentLang = i18n.language;
  return currentLang.split('-')[0] || 'zh';
}

// 获取国家名称（自动使用当前语言）
export function getCountryName(alpha2: string, lang?: string): string {
  if (!alpha2) {
    // 根据当前语言返回相应的"未知地区"文本
    const currentLang = lang || getCurrentLanguage();
    switch (currentLang) {
      case 'en': return "Unknown Region";
      case 'ja': return "未知の地域";
      default: return "未知地区";
    }
  }
  
  const targetLang = lang || getCurrentLanguage();
  return countries.getName(alpha2, targetLang) || alpha2;
}

// 获取当前语言代码（供其他模块使用）
export function getCurrentCountryLanguage(): string {
  return getCurrentLanguage();
}