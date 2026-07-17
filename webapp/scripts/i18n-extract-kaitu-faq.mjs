#!/usr/bin/env node
/**
 * One-shot: extract kaitu-only FAQ items into the kaitu brand overlay.
 * The moved strings intentionally keep 开途/Kaitu/kaitu.io literals — the
 * overlay dir is excluded from the brand-literals guard and from overleap builds.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const localesDir = path.resolve(here, '../src/i18n/locales');
const overlayDir = path.resolve(here, '../src/brands/kaitu/locales');
const KEYS = ['allNationConnect', 'chinaAppStore'];

for (const lang of fs.readdirSync(localesDir)) {
  const basePath = path.join(localesDir, lang, 'ticket.json');
  if (!fs.existsSync(basePath)) continue;
  const base = JSON.parse(fs.readFileSync(basePath, 'utf8'));
  const items = base.faq?.items;
  if (!items) continue;

  const moved = {};
  for (const k of KEYS) {
    if (!items[k]) throw new Error(`${lang}/ticket.json missing faq.items.${k}`);
    moved[k] = items[k];
    delete items[k];
  }

  const outDir = path.join(overlayDir, lang);
  fs.mkdirSync(outDir, { recursive: true });
  fs.writeFileSync(
    path.join(outDir, 'ticket.json'),
    JSON.stringify({ faq: { items: moved } }, null, 2) + '\n'
  );
  fs.writeFileSync(basePath, JSON.stringify(base, null, 2) + '\n');
  console.log(`moved ${KEYS.join('+')} → brand/kaitu/${lang}/ticket.json`);
}
console.log('done');
