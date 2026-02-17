import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { namespaces, defaultNamespace, type Namespace } from './locales/namespaces';

export const languages = {
  'en-US': { nativeName: 'English (US)', countryCode: 'US' },
  'en-GB': { nativeName: 'English (UK)', countryCode: 'GB' },
  'en-AU': { nativeName: 'English (AU)', countryCode: 'AU' },
  'zh-CN': { nativeName: '简体中文', countryCode: 'CN' },
  'zh-TW': { nativeName: '繁體中文', countryCode: 'TW' },
  'zh-HK': { nativeName: '繁體中文 (香港)', countryCode: 'HK' },
  'ja': { nativeName: '日本語', countryCode: 'JP' }
} as const;

export type LanguageCode = keyof typeof languages;

// 动态加载 namespace 的函数
const loadNamespaceResources = async (lang: string, ns: Namespace) => {
  try {
    const module = await import(`./locales/${lang}/${ns}.json`);
    return module.default || module;
  } catch {
    // 回退到默认语言
    const fallbackModule = await import(`./locales/zh-CN/${ns}.json`);
    return fallbackModule.default || fallbackModule;
  }
};

// 预加载默认语言的所有 namespace（用于初始化）
const preloadResources = async (lang: string) => {
  const resources: Record<string, Record<string, unknown>> = {};
  await Promise.all(
    namespaces.map(async (ns) => {
      resources[ns] = await loadNamespaceResources(lang, ns);
    })
  );
  return resources;
};

/**
 * 标准化语言代码，将不支持的语言代码映射到支持的语言
 * 用于确保外部链接使用有效的语言代码
 */
export function normalizeLanguageCode(lang: string): LanguageCode {
  if (lang in languages) {
    return lang as LanguageCode;
  }

  const primaryCode = lang.split('-')[0].toLowerCase();

  const mappings: Record<string, LanguageCode> = {
    'zh': 'zh-CN',
    'zh-sg': 'zh-CN',
    'zh-my': 'zh-CN',
    'zh-hans': 'zh-CN',
    'zh-hant': 'zh-TW',
    'zh-mo': 'zh-HK',
    'en': 'en-US',
    'en-ca': 'en-US',
    'en-nz': 'en-AU',
    'en-za': 'en-GB',
    'en-ie': 'en-GB',
    'ja-jp': 'ja',
  };

  const lowerLang = lang.toLowerCase();
  if (lowerLang in mappings) {
    return mappings[lowerLang];
  }

  if (primaryCode in mappings) {
    return mappings[primaryCode];
  }

  return 'zh-CN';
}

// 初始化 i18n
const initI18n = async () => {
  // 获取当前语言（从 localStorage 或浏览器设置）
  const storedLang = localStorage.getItem('kaitu-language');
  const browserLang = navigator.language;
  const initialLang = normalizeLanguageCode(storedLang || browserLang);

  // 预加载初始语言的所有 namespace
  const initialResources = await preloadResources(initialLang);

  await i18n
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      resources: {
        [initialLang]: initialResources
      },
      lng: initialLang,
      fallbackLng: 'zh-CN',
      defaultNS: defaultNamespace,
      ns: [...namespaces],
      debug: false,

      interpolation: {
        escapeValue: false
      },

      detection: {
        order: ['localStorage', 'navigator'],
        caches: ['localStorage'],
        lookupLocalStorage: 'kaitu-language',
      },

      // 懒加载后端配置
      partialBundledLanguages: true,
    });

  return i18n;
};

// 切换语言时加载新语言的资源
export const changeLanguage = async (lang: LanguageCode) => {
  const normalizedLang = normalizeLanguageCode(lang);

  // 检查是否已经加载了该语言的资源
  const hasResources = namespaces.every(ns =>
    i18n.hasResourceBundle(normalizedLang, ns)
  );

  if (!hasResources) {
    // 加载新语言的所有 namespace
    const resources = await preloadResources(normalizedLang);
    for (const [ns, data] of Object.entries(resources)) {
      i18n.addResourceBundle(normalizedLang, ns, data, true, true);
    }
  }

  await i18n.changeLanguage(normalizedLang);
  localStorage.setItem('kaitu-language', normalizedLang);
};

// 导出初始化 promise
export const i18nPromise = initI18n();

export { namespaces, defaultNamespace, type Namespace };
export default i18n;
