/**
 * 退款同意弹层的说明文案靠 `<privacy>…</privacy>` 标签渲染隐私政策链接
 * （ConsumptionConsentSheet 按标签切分）。任一 locale 漏了标签，链接就静默消失。
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const LOCALES = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../i18n/locales');
const langs = fs.readdirSync(LOCALES).filter((d) => fs.statSync(path.join(LOCALES, d)).isDirectory());

describe('consentNote privacy link', () => {
  it('covers every locale directory', () => {
    expect(langs.length).toBeGreaterThan(20);
  });

  it.each(langs)('%s consentNote carries one <privacy>…</privacy> pair', (lang) => {
    const json = JSON.parse(fs.readFileSync(path.join(LOCALES, lang, 'purchase.json'), 'utf8'));
    const note: string = json.purchase.iap.consentNote;
    expect(typeof note).toBe('string');
    expect(note).toMatch(/<privacy>[^<]+<\/privacy>/);
  });
});
