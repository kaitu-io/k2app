/**
 * Guard: every statically-written i18n key in the source must exist in all 7
 * locale files (base locales merged with the ACTIVE brand's overlay).
 *
 * WHY
 * ---
 * `i18n.ts` configures no `parseMissingKeyHandler`, no `saveMissing` and no
 * `returnNull`, so i18next's default applies: a missing key renders as the key
 * itself. `RouterDevices.tsx` shipped two dialogs whose Cancel button literally
 * read `common:cancel` because the correct key is `common:common.cancel`.
 * Keys that DO pass a default value fail more quietly and are arguably worse:
 * `t('common:common.next', '下一步')` showed Chinese to English users, and the
 * four `errors.vpn.*` codes showed English to Chinese users — for months.
 *
 * WHAT THIS GUARD DOES NOT CATCH
 * ------------------------------
 *  a. DYNAMIC KEYS. 27 call sites build the key from a template literal
 *     (`t(`ticket:faq.items.${key}.question`)`, `t(`smartMode.${opt.labelKey}`)`,
 *     …). They are invisible to any static scan, and several of them resolve
 *     into overlay-only namespaces. Nothing here covers them.
 *  b. Keys reached through a `t` obtained some other way — destructured under a
 *     third alias, passed through a helper, or called as `i18n.t(...)`.
 *     Only `t(` and `tc(` are scanned.
 *  c. Wrong-but-present copy, and untranslated strings that merely exist.
 *  d. Namespace ambiguity for bare keys: a bare `a.b` is accepted if it resolves
 *     under ANY namespace this file imports via `useTranslation('ns')`, plus
 *     `common`, plus `namespaceMapping[a]`. A key that resolves under the wrong
 *     one of those still passes.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { namespaceMapping, defaultNamespace } from '../locales/namespaces';
import { brandConfig } from '../../brands';

const here = path.dirname(fileURLToPath(import.meta.url));
const SRC = path.resolve(here, '../..');
const LOCALES = path.resolve(here, '../locales');
const OVERLAY = path.resolve(SRC, 'brands', brandConfig.id, 'locales');
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

const BUNDLES = Object.fromEntries(LANGS.map((l) => [l, bundleFor(l)]));

function hasIn(bundle: Record<string, any>, ns: string, dotted: string): boolean {
  let cur: any = bundle[ns];
  for (const seg of dotted.split('.')) {
    if (cur == null || typeof cur !== 'object' || !(seg in cur)) return false;
    cur = cur[seg];
  }
  return typeof cur === 'string';
}

/** Remove block and line comments so JSDoc examples are not scanned as code. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

function walk(dir: string, acc: string[] = []): string[] {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === 'node_modules' || e.name === '__tests__') continue;
      walk(p, acc);
    } else if (/\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) {
      acc.push(p);
    }
  }
  return acc;
}

// `t('key')` / `t("key", ...)` / `tc('key')`. Deliberately NOT `\bt\w*\(`,
// which also matches `toContain(`, `toBe(` and friends.
const CALL_RE = /(?<![\w$.])(?:t|tc)\(\s*(['"])([^'"\n]+)\1/g;
const KEY_RE = /^[a-zA-Z][\w-]*(?::[\w.-]+)?(?:\.[\w.-]+)*$/;

interface Ref { key: string; file: string; line: number; namespaces: string[] }

function collect(): Ref[] {
  const refs: Ref[] = [];
  for (const file of walk(SRC)) {
    const raw = fs.readFileSync(file, 'utf8');
    const src = stripComments(raw);
    const explicit = [...src.matchAll(/useTranslation\(\s*['"]([^'"]+)['"]/g)].map((m) => m[1]);
    let m: RegExpExecArray | null;
    CALL_RE.lastIndex = 0;
    while ((m = CALL_RE.exec(src))) {
      const key = m[2];
      if (!KEY_RE.test(key)) continue;
      if (!key.includes('.') && !key.includes(':')) continue;
      const line = src.slice(0, m.index).split('\n').length;
      let namespaces: string[];
      if (key.includes(':')) {
        namespaces = [key.slice(0, key.indexOf(':'))];
      } else {
        namespaces = [
          ...new Set(
            [...explicit, defaultNamespace, namespaceMapping[key.split('.')[0]]].filter(Boolean)
          ),
        ] as string[];
      }
      refs.push({ key, file: path.relative(SRC, file), line, namespaces });
    }
  }
  return refs;
}

const REFS = collect();

describe('static i18n keys resolve in every locale', () => {
  it('scanned a plausible number of call sites (guards the scanner itself)', () => {
    // A regex that stops matching turns every assertion below into a no-op.
    expect(REFS.length).toBeGreaterThan(500);
    expect(REFS.some((r) => r.key === 'common:common.cancel')).toBe(true);
  });

  it('every statically-written key exists in all 7 locales', () => {
    const missing: string[] = [];
    for (const ref of REFS) {
      const dotted = ref.key.includes(':') ? ref.key.slice(ref.key.indexOf(':') + 1) : ref.key;
      for (const lang of LANGS) {
        if (!ref.namespaces.some((ns) => hasIn(BUNDLES[lang], ns, dotted))) {
          missing.push(`${ref.file}:${ref.line}  ${ref.key}  @${lang}`);
        }
      }
    }
    expect([...new Set(missing)].sort()).toEqual([]);
  });
});
