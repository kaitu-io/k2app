/**
 * Pick a supported locale from an ordered list of preferred BCP 47 tags
 * (navigator.languages). Pure and brand-agnostic: callers pass the brand's
 * locale whitelist and default.
 *
 * Per preferred tag, in order:
 *   1. the tag itself, then each parent (`zh-Hant-HK` → `zh-Hant` → `zh`),
 *      as an exact supported locale or through ALIASES;
 *   2. the first supported locale sharing the primary language
 *      (`pt-PT` → `pt-BR`, `es-MX` → `es`).
 * Only when no preferred tag matches does the fallback apply.
 */

/** Region/script variants whose best match is not "first locale of the same
 *  language". Keys are lowercase. An alias only applies when its target is in
 *  the supported list. */
const ALIASES: Readonly<Record<string, string>> = {
  zh: 'zh-CN',
  'zh-hans': 'zh-CN',
  'zh-sg': 'zh-CN',
  'zh-my': 'zh-CN',
  'zh-hant': 'zh-TW',
  'zh-hant-hk': 'zh-HK',
  'zh-hant-mo': 'zh-HK',
  'zh-mo': 'zh-HK',
  en: 'en-US',
  'en-ca': 'en-US',
  'en-nz': 'en-AU',
  'en-za': 'en-GB',
  'en-ie': 'en-GB',
  'en-in': 'en-GB',
  // Legacy ISO 639 codes still reported by some Android WebViews.
  in: 'id',
};

export function matchLocale<T extends string>(
  preferred: readonly string[],
  supported: readonly T[],
  fallback: T,
): T {
  const byLower = new Map(supported.map((l) => [l.toLowerCase(), l] as const));

  for (const raw of preferred) {
    if (!raw) continue;
    const tag = raw.trim().toLowerCase().replace(/_/g, '-');
    if (!tag) continue;

    for (let t = tag; t; t = t.includes('-') ? t.slice(0, t.lastIndexOf('-')) : '') {
      const exact = byLower.get(t);
      if (exact) return exact;
      const alias = ALIASES[t];
      const aliased = alias && byLower.get(alias.toLowerCase());
      if (aliased) return aliased;
    }

    const primary = tag.split('-')[0];
    const sameLanguage = supported.find((l) => l.toLowerCase().split('-')[0] === primary);
    if (sameLanguage) return sameLanguage;
  }

  return fallback;
}
