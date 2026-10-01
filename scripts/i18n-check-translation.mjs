#!/usr/bin/env node
// Structural check for a translated locale against its English master.
//   node scripts/i18n-check-translation.mjs <lang> [<lang> ...]
// Verifies, per file: valid JSON, identical key set, no empty strings, and that
// every value keeps the master's placeholders and markup tags verbatim. It cannot judge translation quality — only that nothing a
// translator must not touch was touched.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const TARGETS = [
  { name: 'webapp', dir: 'webapp/src/i18n/locales', master: 'en-US' },
  { name: 'site', dir: 'sites/overleap/messages', master: 'en-GB' },
];

// Tokens a translator must carry over exactly: interpolation placeholders,
// markup tags, nested-key references. Brand variables are exempt — they are
// i18next default variables, valid in any string, and a sentence may
// legitimately name the brand in one language and not in another.
const TOKEN_RE = /\{\{[^}]+\}\}|\{[^{}]+\}|<\/?[a-zA-Z0-9]+[^>]*>|\$t\([^)]*\)/g;
const BRAND_VARS = new Set(['{{brand}}', '{{brandDomain}}', '{{brandBaseUrl}}', '{{supportEmail}}']);
const tokens = (s) =>
  (s.match(TOKEN_RE) ?? [])
    .filter((t) => !BRAND_VARS.has(t))
    .sort()
    .join('|');

function flatten(obj, prefix = '', out = {}) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, key, out);
    else out[key] = v;
  }
  return out;
}

/** @param only restrict to some targets by name ('webapp' | 'site'); default all. */
export function checkLocale(lang, only) {
  const problems = [];
  for (const t of TARGETS) {
    if (only && !only.includes(t.name)) continue;
    const masterDir = path.join(root, t.dir, t.master);
    for (const file of fs.readdirSync(masterDir).filter((f) => f.endsWith('.json'))) {
      const rel = `${t.dir}/${lang}/${file}`;
      const target = path.join(root, rel);
      if (!fs.existsSync(target)) {
        problems.push(`${rel}: missing file`);
        continue;
      }
      let got;
      try {
        got = flatten(JSON.parse(fs.readFileSync(target, 'utf8')));
      } catch (e) {
        problems.push(`${rel}: invalid JSON (${e.message})`);
        continue;
      }
      const want = flatten(JSON.parse(fs.readFileSync(path.join(masterDir, file), 'utf8')));
      for (const k of Object.keys(want)) if (!(k in got)) problems.push(`${rel}: missing key ${k}`);
      for (const k of Object.keys(got)) if (!(k in want)) problems.push(`${rel}: extra key ${k}`);
      for (const [k, v] of Object.entries(want)) {
        if (!(k in got)) continue;
        const g = got[k];
        if (typeof g !== typeof v || Array.isArray(g) !== Array.isArray(v)) {
          problems.push(`${rel}: ${k} changed type`);
        } else if (typeof v === 'string') {
          if (v.trim() && !g.trim()) problems.push(`${rel}: ${k} is empty`);
          if (tokens(v) !== tokens(g)) {
            problems.push(`${rel}: ${k} placeholders/tags differ — master [${tokens(v)}] got [${tokens(g)}]`);
          }
        } else if (Array.isArray(v) && v.length !== g.length) {
          problems.push(`${rel}: ${k} array length differs`);
        }
      }
    }
  }
  return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const langs = process.argv.slice(2);
  if (!langs.length) {
    console.error('usage: i18n-check-translation.mjs <lang> [<lang> ...]');
    process.exit(2);
  }
  let bad = 0;
  for (const lang of langs) {
    const problems = checkLocale(lang);
    bad += problems.length;
    console.log(problems.length ? `${lang}: ${problems.length} problem(s)\n  ${problems.join('\n  ')}` : `${lang}: OK`);
  }
  process.exit(bad ? 1 : 0);
}
