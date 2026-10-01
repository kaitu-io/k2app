import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { namespaces, defaultNamespace, type Namespace } from './locales/namespaces';
import { brandConfig } from '../brands';
import { brandI18nVariables } from '../brands/i18n-vars';
import { matchLocale } from './match-locale';

/**
 * Every language the webapp carries copy for. Which of these a build actually
 * OFFERS is the brand's call — `brandConfig.locales` (see availableLanguages).
 * `englishName` exists so the picker's search matches "korean" as well as
 * "한국어". No flags: a language is not a country.
 */
export const languages = {
  'en-US': { nativeName: 'English (US)', englishName: 'English (US)', dir: 'ltr' },
  'en-GB': { nativeName: 'English (UK)', englishName: 'English (UK)', dir: 'ltr' },
  'en-AU': { nativeName: 'English (AU)', englishName: 'English (Australia)', dir: 'ltr' },
  'zh-CN': { nativeName: '简体中文', englishName: 'Chinese (Simplified)', dir: 'ltr' },
  'zh-TW': { nativeName: '繁體中文', englishName: 'Chinese (Traditional)', dir: 'ltr' },
  'zh-HK': { nativeName: '繁體中文 (香港)', englishName: 'Chinese (Hong Kong)', dir: 'ltr' },
  'ja': { nativeName: '日本語', englishName: 'Japanese', dir: 'ltr' },
  'ko': { nativeName: '한국어', englishName: 'Korean', dir: 'ltr' },
  'es': { nativeName: 'Español', englishName: 'Spanish', dir: 'ltr' },
  'pt-BR': { nativeName: 'Português (Brasil)', englishName: 'Portuguese (Brazil)', dir: 'ltr' },
  'fr': { nativeName: 'Français', englishName: 'French', dir: 'ltr' },
  'de': { nativeName: 'Deutsch', englishName: 'German', dir: 'ltr' },
  'it': { nativeName: 'Italiano', englishName: 'Italian', dir: 'ltr' },
  'ru': { nativeName: 'Русский', englishName: 'Russian', dir: 'ltr' },
  'tr': { nativeName: 'Türkçe', englishName: 'Turkish', dir: 'ltr' },
  'ar': { nativeName: 'العربية', englishName: 'Arabic', dir: 'rtl' },
  'fa': { nativeName: 'فارسی', englishName: 'Persian', dir: 'rtl' },
  'id': { nativeName: 'Bahasa Indonesia', englishName: 'Indonesian', dir: 'ltr' },
  'ms': { nativeName: 'Bahasa Melayu', englishName: 'Malay', dir: 'ltr' },
  'vi': { nativeName: 'Tiếng Việt', englishName: 'Vietnamese', dir: 'ltr' },
  'th': { nativeName: 'ไทย', englishName: 'Thai', dir: 'ltr' },
  'my': { nativeName: 'မြန်မာ', englishName: 'Burmese', dir: 'ltr' },
  'km': { nativeName: 'ខ្មែរ', englishName: 'Khmer', dir: 'ltr' },
} as const satisfies Record<string, { nativeName: string; englishName: string; dir: 'ltr' | 'rtl' }>;

export type LanguageCode = keyof typeof languages;

/** The languages THIS brand offers, in the brand's display order. */
export const availableLanguages: readonly LanguageCode[] = brandConfig.locales;

export function languageDirection(lang: string): 'ltr' | 'rtl' {
  return languages[lang as LanguageCode]?.dir ?? 'ltr';
}

/** Case-insensitive substring filter over native name, English name and code. */
export function filterLanguages(query: string, codes: readonly LanguageCode[] = availableLanguages): LanguageCode[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...codes];
  return codes.filter((code) => {
    const { nativeName, englishName } = languages[code];
    return (
      nativeName.toLowerCase().includes(q) ||
      englishName.toLowerCase().includes(q) ||
      code.toLowerCase().includes(q)
    );
  });
}

/** Shallow-recursive merge: overlay wins; objects merge, scalars/arrays replace. */
function deepMerge<T extends Record<string, any>>(base: T, overlay: Record<string, any>): T {
  const out: Record<string, any> = { ...base };
  for (const [k, v] of Object.entries(overlay)) {
    out[k] =
      v && typeof v === 'object' && !Array.isArray(v) && typeof out[k] === 'object'
        ? deepMerge(out[k], v)
        : v;
  }
  return out as T;
}

// The brand segment must be a compile-time constant in each import template so
// Vite's import-analysis only bundles the ACTIVE brand's overlay JSONs — the
// dead branch is removed after __K2_BRAND__ define folding (artifact purity).
declare const __K2_BRAND__: string;
const loadBrandOverlay: (lang: string, ns: Namespace) => Promise<any> =
  __K2_BRAND__ === 'overleap'
    ? (lang, ns) => import(`../brands/overleap/locales/${lang}/${ns}.json`)
    : (lang, ns) => import(`../brands/kaitu/locales/${lang}/${ns}.json`);

// 动态加载 namespace 的函数
const loadNamespaceResources = async (lang: string, ns: Namespace) => {
  let base: Record<string, any>;
  try {
    const module = await import(`./locales/${lang}/${ns}.json`);
    base = module.default || module;
  } catch {
    // 回退到品牌默认语言
    const fallbackModule = await import(`./locales/${brandConfig.defaultLocale}/${ns}.json`);
    base = fallbackModule.default || fallbackModule;
  }
  try {
    const overlay = await loadBrandOverlay(lang, ns);
    return deepMerge(base, overlay.default || overlay);
  } catch {
    return base; // no overlay for this brand/lang/ns — normal case
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
 * 标准化语言代码：把任意 BCP 47 标签映射到本品牌提供的语言，映射不到落品牌默认语言。
 * 用于确保外部链接、已保存的偏好使用有效的语言代码。
 */
export function normalizeLanguageCode(lang: string): LanguageCode {
  return matchLocale([lang], availableLanguages, brandConfig.defaultLocale);
}

const STORAGE_KEY = 'kaitu-language';

/**
 * Boot language: an explicit earlier choice wins if this brand still offers it;
 * otherwise walk the system's ordered language list (not just its first entry,
 * so "fr, en" on a build without French still lands on English by preference
 * rather than by default).
 */
export function detectInitialLanguage(
  stored: string | null,
  systemLanguages: readonly string[],
): LanguageCode {
  if (stored && (availableLanguages as readonly string[]).includes(stored)) {
    return stored as LanguageCode;
  }
  return matchLocale(systemLanguages, availableLanguages, brandConfig.defaultLocale);
}

function applyDocumentLanguage(lang: string) {
  document.documentElement.lang = lang;
  document.documentElement.dir = languageDirection(lang);
}

// 初始化 i18n
const initI18n = async () => {
  // 获取当前语言（从 localStorage 或浏览器设置）
  const systemLanguages = navigator.languages?.length ? navigator.languages : [navigator.language];
  const initialLang = detectInitialLanguage(localStorage.getItem(STORAGE_KEY), systemLanguages);

  // 预加载初始语言的所有 namespace
  const initialResources = await preloadResources(initialLang);

  // No i18next language detector: it would persist the auto-detected language
  // as if the user had chosen it, pinning the app to whatever the system
  // language was on first launch. Only changeLanguage() writes the preference.
  await i18n
    .use(initReactI18next)
    .init({
      resources: {
        [initialLang]: initialResources
      },
      lng: initialLang,
      fallbackLng: brandConfig.defaultLocale,
      defaultNS: defaultNamespace,
      ns: [...namespaces],
      debug: false,

      interpolation: {
        escapeValue: false,
        defaultVariables: brandI18nVariables(initialLang),
      },

      // 懒加载后端配置
      partialBundledLanguages: true,
    });

  // Brand name is locale-dependent (kaitu: 开途 in zh-*, Kaitu elsewhere) —
  // refresh interpolation defaults whenever the language changes.
  i18n.on('languageChanged', (lng) => {
    if (i18n.options.interpolation) {
      i18n.options.interpolation.defaultVariables = brandI18nVariables(lng);
    }
    applyDocumentLanguage(lng);
  });
  applyDocumentLanguage(initialLang);

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
  localStorage.setItem(STORAGE_KEY, normalizedLang);
};

// 导出初始化 promise
export const i18nPromise = initI18n();

export { namespaces, defaultNamespace, type Namespace };
export default i18n;
