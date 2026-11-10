// Static analysis behind src/i18n/client-messages.generated.ts.
//
// Question answered: which message paths does the CLIENT side of each route use?
// Server components translate on the server and ship text, not messages; only
// code that runs in the browser needs messages in the page payload.
//
// Method: from a route's page/layout entry, walk the import graph. The walk
// starts "server"; at a file with a "use client" directive everything beneath it
// is client. For every client file, collect each string literal (or template
// head) that begins with a message path that really exists in the zh-CN tree,
// plus useTranslations('scope') scopes. A message path is `<file>.<topKey>`
// (messages/<locale>/<file>.json is mounted under its file name, see
// src/i18n/request.ts), so 2 segments is the unit. A one-segment scope is the
// whole file.
//
// It over-approximates on purpose (a string that merely looks like a path adds a
// few KB at worst) and never reads types. Keys built entirely at runtime with no
// literal in the source are invisible to it — the live crawl and the browser
// pass in web/CLAUDE.md cover those.
import fs from 'node:fs';
import path from 'node:path';

export function createAnalyzer(webRoot, referenceLocale = 'zh-CN') {
  const SRC = path.join(webRoot, 'src');
  const APP = path.join(SRC, 'app', '[locale]');

  const tree = {};
  const msgDir = path.join(webRoot, 'messages', referenceLocale);
  for (const f of fs.readdirSync(msgDir)) {
    if (f.endsWith('.json')) tree[f.slice(0, -5)] = JSON.parse(fs.readFileSync(path.join(msgDir, f), 'utf8'));
  }
  const exists = (segs) => {
    let n = tree;
    for (const s of segs) {
      if (n && typeof n === 'object' && s in n) n = n[s];
      else return false;
    }
    return true;
  };

  const cache = new Map();
  const read = (f) => {
    if (!cache.has(f)) cache.set(f, fs.readFileSync(f, 'utf8'));
    return cache.get(f);
  };
  const isClient = (src) => /^\s*(['"])use client\1/m.test(src.slice(0, 400));

  const resolveImport = (from, spec) => {
    let base;
    if (spec.startsWith('@/')) base = path.join(SRC, spec.slice(2));
    else if (spec.startsWith('.')) base = path.resolve(path.dirname(from), spec);
    else return null;
    for (const c of [base + '.tsx', base + '.ts', path.join(base, 'index.tsx'), path.join(base, 'index.ts'), base]) {
      if (fs.existsSync(c) && fs.statSync(c).isFile()) return c;
    }
    return null;
  };
  const importsOf = (f) => {
    const out = new Set();
    for (const m of read(f).matchAll(
      /(?:import|export)[^'"`;]*?from\s*['"]([^'"]+)['"]|import\(\s*['"]([^'"]+)['"]\s*\)|import\s*['"]([^'"]+)['"]/g
    )) {
      const r = resolveImport(f, m[1] || m[2] || m[3]);
      if (r) out.add(r);
    }
    return [...out];
  };

  const keysIn = (f) => {
    const s = read(f);
    const out = new Set();
    for (const m of s.matchAll(/useTranslations\(\s*['"]([^'"]+)['"]\s*\)/g)) {
      out.add(m[1].split('.').slice(0, 2).join('.'));
    }
    for (const m of s.matchAll(/['"`]([A-Za-z0-9-]+(?:\.[A-Za-z0-9_-]+)+)/g)) {
      const segs = m[1].split('.');
      if (exists(segs.slice(0, 2))) out.add(segs.slice(0, 2).join('.'));
    }
    return out;
  };

  /** Message paths used by the client side of everything reachable from `entry`. */
  const clientKeys = (entry) => {
    const seen = new Set();
    const keys = new Set();
    const go = (f, client) => {
      const id = `${f}:${client ? 'c' : 's'}`;
      if (seen.has(id)) return;
      seen.add(id);
      const c = client || isClient(read(f));
      if (c) for (const k of keysIn(f)) keys.add(k);
      for (const i of importsOf(f)) go(i, c);
    };
    go(entry, false);
    return keys;
  };

  /** Drop a path when a shorter one (its file) already covers it. */
  const normalize = (keys) => {
    const set = new Set(keys);
    // A file where most top-level keys are used anyway (errors.*, survey.*) is
    // listed whole: a 20-line list says nothing a file name doesn't.
    const byFile = {};
    for (const k of set) {
      const [file, top] = k.split('.');
      if (top !== undefined) (byFile[file] ||= new Set()).add(top);
    }
    const bytes = (v) => JSON.stringify(v).length;
    for (const [file, tops] of Object.entries(byFile)) {
      const all = tree[file] ?? {};
      if (Object.keys(all).length < 4) continue;
      const used = [...tops].reduce((n, t) => n + bytes(t) + bytes(all[t]), 0);
      if (used / bytes(all) >= 0.6) set.add(file);
    }
    return [...set].filter((k) => !k.includes('.') || !set.has(k.split('.')[0])).sort();
  };

  // Next.js special files that render inside a route's provider.
  const SPECIAL = /^(page|layout|template|error|loading|not-found|default)\.tsx$/;

  const entriesIn = (dir) => {
    const out = [];
    const walk = (d) => {
      for (const e of fs.readdirSync(d, { withFileTypes: true })) {
        const p = path.join(d, e.name);
        if (e.isDirectory()) walk(p);
        else if (SPECIAL.test(e.name)) out.push(p);
      }
    };
    walk(dir);
    return out;
  };

  /**
   * BASE = what the root layout's providers and the shared page chrome
   * (Header / Footer — rendered by every page) need.
   * GROUPS = per top-level route directory, the extra paths beyond BASE.
   */
  const analyze = () => {
    const chrome = ['Header.tsx', 'Footer.tsx'].map((n) => path.join(SRC, 'components', n));
    const base = new Set(clientKeys(path.join(APP, 'layout.tsx')));
    for (const c of chrome) for (const k of clientKeys(c)) base.add(k);
    // clientKeys() starts "server"; chrome files are client components themselves,
    // but make sure their own keys count even if a refactor drops the directive.
    for (const c of chrome) for (const k of keysIn(c)) base.add(k);

    const baseN = normalize([...base]);
    const baseSet = new Set(baseN);
    const groups = {};
    for (const e of fs.readdirSync(APP, { withFileTypes: true })) {
      if (!e.isDirectory()) continue;
      const keys = new Set();
      for (const entry of entriesIn(path.join(APP, e.name))) for (const k of clientKeys(entry)) keys.add(k);
      const extra = normalize([...keys].filter((k) => !coveredBy(k, baseSet)));
      if (extra.length) groups[e.name] = extra;
    }
    // Files directly under [locale]/ (the home page, error.tsx ...) sit under the
    // root provider, which is BASE only: whatever they need beyond it is a bug.
    const rootKeys = new Set();
    for (const e of fs.readdirSync(APP, { withFileTypes: true })) {
      if (e.isFile() && SPECIAL.test(e.name) && e.name !== 'layout.tsx') {
        for (const k of clientKeys(path.join(APP, e.name))) rootKeys.add(k);
      }
    }
    const rootExtra = normalize([...rootKeys].filter((k) => !coveredBy(k, baseSet)));
    return { base: baseN, groups, rootExtra };
  };

  const coveredBy = (k, set) => {
    if (set.has(k)) return true;
    return set.has(k.split('.')[0]);
  };

  return { analyze, tree };
}

export function renderGenerated({ base, groups }) {
  const fmt = (a) => `[${a.map((s) => JSON.stringify(s)).join(', ')}]`;
  const lines = [
    '// Auto-generated by scripts/i18n/gen-client-messages.mjs',
    "// DO NOT EDIT - run 'node scripts/i18n/gen-client-messages.mjs' to regenerate.",
    '// Checked by tests/client-messages.test.ts (the file must equal a fresh analysis).',
    '',
    '/** Message paths the root providers and shared page chrome (Header/Footer) need. */',
    `export const BASE_MESSAGE_PATHS: readonly string[] = ${fmt(base)};`,
    '',
    '/** Per top-level route directory: paths needed beyond BASE_MESSAGE_PATHS. */',
    'export const GROUP_MESSAGE_PATHS: Record<string, readonly string[]> = {',
    ...Object.keys(groups)
      .sort()
      .map((g) => `  ${JSON.stringify(g)}: ${fmt(groups[g])},`),
    '};',
    '',
  ];
  return lines.join('\n');
}
