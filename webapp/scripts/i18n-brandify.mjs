#!/usr/bin/env node
/**
 * One-shot codemod: replace brand literals in base locale JSONs with i18next
 * interpolation placeholders (resolved via defaultVariables — see
 * src/brand/i18n-vars.ts). Order matters: domains before bare names.
 * Does NOT touch src/brands/<brand>/locales/ overlays.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src/i18n/locales');
const rules = [
  [/Kaitu\.io/g, '{{brandDomain}}'],
  [/kaitu\.io/g, '{{brandDomain}}'],
  [/Overleap\.io/g, '{{brandDomain}}'], // legacy drift, e.g. invite.inviteYouToUse
  [/開途/g, '{{brand}}'],
  [/开途/g, '{{brand}}'],
  [/Kaitu/g, '{{brand}}'],
  [/Overleap/g, '{{brand}}'],
];

let changed = 0;
for (const lang of fs.readdirSync(root)) {
  const dir = path.join(root, lang);
  if (!fs.statSync(dir).isDirectory()) continue;
  for (const f of fs.readdirSync(dir)) {
    if (!f.endsWith('.json')) continue;
    const p = path.join(dir, f);
    const before = fs.readFileSync(p, 'utf8');
    let after = before;
    for (const [re, sub] of rules) after = after.replace(re, sub);
    if (after !== before) {
      JSON.parse(after); // sanity: still valid JSON
      fs.writeFileSync(p, after);
      changed++;
      console.log('brandified', path.relative(root, p));
    }
  }
}
console.log(`done — ${changed} files changed`);
