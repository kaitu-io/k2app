/**
 * Coverage guard for the error-code → i18n catalog.
 *
 * WHY THIS EXISTS
 * ---------------
 * Error copy used to live in two independently hand-written maps: a 69-case
 * `switch` in `getErrorMessage()` (API/response domain) and a 9-entry object
 * literal in `getErrorI18nKey()` (VPN/engine domain). The engine map silently
 * lagged: 40 of the 47 codes declared in the 100–599 range had no entry, so a
 * real connection failure rendered "未知错误" even though precise copy already
 * existed one module over. One of the 9 entries it *did* have carried the wrong
 * numeric value (408 instead of the engine's 108), which made both the copy and
 * `isNetworkError()` wrong for every timeout.
 *
 * `errorCatalog.ts` is now the single declaration; both functions derive from
 * it. This file is the gate that keeps the declaration honest.
 *
 * WHAT THIS GUARD CATCHES
 * -----------------------
 *  1. A catalog entry whose i18n key is absent from any of the 7 locales
 *     (checked against base locales merged with the ACTIVE brand's overlay).
 *  2. A code in ERROR_CODES with no API catalog entry, unless it is on the
 *     explicit LOG_ONLY_CODES allowlist.
 *  3. An engine code declared in ENGINE_ERROR_CODES with no engine catalog entry.
 *  4. `getErrorI18nKey()` falling back to the unknown-error key for any code the
 *     engine catalog claims to cover.
 *  5. A catalog entry with an empty/absent English default.
 *
 * WHAT THIS GUARD DOES **NOT** CATCH — read before trusting a green run
 * --------------------------------------------------------------------
 *  a. Wrong-but-present copy. It asserts a key *resolves*, never that the text
 *     is right for the code, or that a translation is not just the English
 *     string copy-pasted.
 *  b. Codes that exist in the Go engine but were never added to
 *     ENGINE_ERROR_CODES at all. The k2 cross-check in
 *     `src/services/__tests__/k2-engine-codes.test.ts` covers that — but only
 *     where the `k2/` submodule is checked out. CI's webapp job runs
 *     `actions/checkout@v4` WITHOUT submodules, so that check is skipped there.
 *  c. Codes produced by the native bridges / daemon that no TypeScript file
 *     declares. Nothing in this repo enumerates them.
 *  d. Dynamic i18n keys (template strings). See the note in
 *     `src/i18n/__tests__/static-keys.test.ts`.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  ERROR_CODES,
  ENGINE_ERROR_CODES,
  API_ERROR_CATALOG,
  ENGINE_ERROR_CATALOG,
  LOG_ONLY_CODES,
  ENGINE_NETWORK_ERROR_CODES,
} from '../errorCatalog';
import { getErrorI18nKey, isNetworkError } from '../../services/vpn-types';
import { brandConfig } from '../../brands';

const here = path.dirname(fileURLToPath(import.meta.url));
const LOCALES = path.resolve(here, '../../i18n/locales');
const OVERLAY = path.resolve(here, '../../brands', brandConfig.id, 'locales');
const LANGS = ['en-AU', 'en-GB', 'en-US', 'ja', 'zh-CN', 'zh-HK', 'zh-TW'] as const;

function deepMerge(a: any, b: any): any {
  const out = { ...a };
  for (const [k, v] of Object.entries(b)) {
    out[k] =
      v && typeof v === 'object' && !Array.isArray(v) && typeof out[k] === 'object'
        ? deepMerge(out[k], v)
        : v;
  }
  return out;
}

/** base locale JSONs for `lang`, merged with the active brand's overlay. */
function bundleFor(lang: string): Record<string, any> {
  const out: Record<string, any> = {};
  const baseDir = path.join(LOCALES, lang);
  for (const f of fs.readdirSync(baseDir)) {
    if (!f.endsWith('.json')) continue;
    out[f.slice(0, -5)] = JSON.parse(fs.readFileSync(path.join(baseDir, f), 'utf8'));
  }
  const ovDir = path.join(OVERLAY, lang);
  if (fs.existsSync(ovDir)) {
    for (const f of fs.readdirSync(ovDir)) {
      if (!f.endsWith('.json')) continue;
      const ns = f.slice(0, -5);
      out[ns] = deepMerge(out[ns] ?? {}, JSON.parse(fs.readFileSync(path.join(ovDir, f), 'utf8')));
    }
  }
  return out;
}

const BUNDLES = Object.fromEntries(LANGS.map((l) => [l, bundleFor(l)])) as Record<string, any>;

/** Resolve a fully-qualified `ns:a.b.c` key inside one language bundle. */
export function resolveKey(bundle: Record<string, any>, fq: string): string | undefined {
  const i = fq.indexOf(':');
  const ns = i >= 0 ? fq.slice(0, i) : 'common';
  const dotted = i >= 0 ? fq.slice(i + 1) : fq;
  let cur: any = bundle[ns];
  for (const seg of dotted.split('.')) {
    if (cur == null || typeof cur !== 'object' || !(seg in cur)) return undefined;
    cur = cur[seg];
  }
  return typeof cur === 'string' ? cur : undefined;
}

const ALL_ENTRIES = [
  ...Object.entries(API_ERROR_CATALOG).map(([c, e]) => ['api', Number(c), e] as const),
  ...Object.entries(ENGINE_ERROR_CATALOG).map(([c, e]) => ['engine', Number(c), e] as const),
];

describe('error catalog — i18n key coverage', () => {
  it('every catalog key is fully qualified (ns:path)', () => {
    const bad = ALL_ENTRIES.filter(([, , e]) => !/^[a-zA-Z]+:.+/.test(e.key));
    expect(bad.map(([d, c, e]) => `${d}/${c} -> ${e.key}`)).toEqual([]);
  });

  it('every catalog key resolves in all 7 locales', () => {
    const missing: string[] = [];
    for (const [domain, code, entry] of ALL_ENTRIES) {
      for (const lang of LANGS) {
        if (resolveKey(BUNDLES[lang], entry.key) === undefined) {
          missing.push(`${domain}/${code} ${entry.key} @ ${lang}`);
        }
      }
    }
    expect(missing).toEqual([]);
  });

  it('every catalog entry carries a non-empty English default', () => {
    const bad = ALL_ENTRIES.filter(([, , e]) => !e.defaultValue || !e.defaultValue.trim());
    expect(bad.map(([d, c]) => `${d}/${c}`)).toEqual([]);
  });
});

describe('error catalog — code coverage', () => {
  it('every ERROR_CODES value has an API catalog entry or is explicitly log-only', () => {
    const uncovered = Object.entries(ERROR_CODES)
      .filter(([, code]) => !(code in API_ERROR_CATALOG) && !LOG_ONLY_CODES.includes(code as never))
      .map(([name, code]) => `${name}=${code}`);
    expect(uncovered).toEqual([]);
  });

  it('every k2 engine code has an engine catalog entry', () => {
    const uncovered = Object.entries(ENGINE_ERROR_CODES)
      .filter(([, code]) => !(code in ENGINE_ERROR_CATALOG))
      .map(([name, code]) => `${name}=${code}`);
    expect(uncovered).toEqual([]);
  });

  it('getErrorI18nKey returns the catalog key — never the unknown fallback — for every covered code', () => {
    const fallbacks: string[] = [];
    for (const [code, entry] of Object.entries(ENGINE_ERROR_CATALOG)) {
      const got = getErrorI18nKey(Number(code));
      if (got !== entry.key) fallbacks.push(`${code}: got ${got}, want ${entry.key}`);
    }
    expect(fallbacks).toEqual([]);
  });

  it('getErrorI18nKey still falls back for a code nobody declares', () => {
    // Guards the guard: if the fallback were removed the assertion above would
    // pass vacuously for any input.
    expect(getErrorI18nKey(999999)).toBe('common:errors.unknown');
  });
});

describe('engine timeout is 108, not 408', () => {
  // k2/engine/error.go: ErrCodeTimeout = 108. This file claimed 408 until
  // 2026-08-19, which routed every handshake timeout to "未知错误" AND made
  // isNetworkError() answer false for it.
  it('108 resolves to the timeout copy', () => {
    expect(getErrorI18nKey(108)).toBe('common:errors.network.timeout');
  });

  it('408 is NOT an engine code and gets no engine copy', () => {
    expect(getErrorI18nKey(408)).toBe('common:errors.unknown');
  });

  it('isNetworkError covers 108 and no longer covers 408', () => {
    expect(isNetworkError(108)).toBe(true);
    expect(isNetworkError(408)).toBe(false);
  });

  it('isNetworkError set matches the declared network-code set', () => {
    for (const c of ENGINE_NETWORK_ERROR_CODES) expect(isNetworkError(c)).toBe(true);
    expect(isNetworkError(570)).toBe(false);
  });
});

describe('573 — auto-pick pool emptied by the country filter', () => {
  // Dispatched by connection.store.ts. The copy must tell the user what to do
  // (relax the filter), not just that something broke.
  it('maps to the actionable allExcluded copy in both domains', () => {
    expect(getErrorI18nKey(573)).toBe('dashboard:auto.allExcluded');
    expect(API_ERROR_CATALOG[573].key).toBe('dashboard:auto.allExcluded');
  });

  it('the zh-CN and en-US copy names the country filter as the fix', () => {
    expect(resolveKey(BUNDLES['zh-CN'], 'dashboard:auto.allExcluded')).toMatch(/过滤|排除/);
    expect(resolveKey(BUNDLES['en-US'], 'dashboard:auto.allExcluded')).toMatch(/filter/i);
  });
});
