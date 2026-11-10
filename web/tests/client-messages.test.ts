/**
 * Guards for the client-side message split (src/i18n/ClientMessages.tsx).
 *
 * Every page used to embed ALL translations (≈ 55 KB gzip, 300-450 KB raw, per
 * page) and that blew the Amplify output cap. Now each route tree gets only what
 * its client components use. These tests are what makes that safe: a client
 * component that starts using a key its route doesn't provide renders raw key
 * text in production, and no component test would notice (useTranslations is
 * stubbed to identity in src/test/setup.ts).
 */
import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { createAnalyzer, renderGenerated } from '../scripts/i18n/client-messages-lib.mjs';
import { BASE_MESSAGE_PATHS, GROUP_MESSAGE_PATHS } from '../src/i18n/client-messages.generated';
import { KAITU } from '../src/lib/brands';

const WEB = path.resolve(__dirname, '..');
const APP = path.join(WEB, 'src', 'app', '[locale]');

const layoutFiles = (dir: string): string[] => {
  const out: string[] = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...layoutFiles(p));
    else if (e.name === 'layout.tsx') out.push(p);
  }
  return out;
};

describe('client-messages.generated.ts', () => {
  it('equals a fresh analysis of the source — run node scripts/i18n/gen-client-messages.mjs if this fails', () => {
    const fresh = renderGenerated(createAnalyzer(WEB).analyze());
    const committed = fs.readFileSync(path.join(WEB, 'src', 'i18n', 'client-messages.generated.ts'), 'utf8');
    expect(committed).toBe(fresh);
  });

  it('every listed path exists in every served locale (a gap would silently drop text)', () => {
    const paths = [...BASE_MESSAGE_PATHS, ...Object.values(GROUP_MESSAGE_PATHS).flat()];
    for (const locale of KAITU.allowedLocales) {
      const dir = path.join(WEB, 'messages', locale);
      for (const p of new Set(paths)) {
        const [file, top] = p.split('.');
        const f = path.join(dir, `${file}.json`);
        // request.ts falls back to zh-CN per missing file, so only the fallback must have it.
        const source = fs.existsSync(f) ? f : path.join(WEB, 'messages', 'zh-CN', `${file}.json`);
        const json = JSON.parse(fs.readFileSync(source, 'utf8'));
        if (top !== undefined) expect(Object.keys(json), `${locale}: ${p}`).toContain(top);
      }
    }
  });
});

describe('route wiring', () => {
  const layouts = layoutFiles(APP).map((f) => ({ f, src: fs.readFileSync(f, 'utf8') }));

  it('every group has a layout under its directory that provides exactly that group', () => {
    for (const group of Object.keys(GROUP_MESSAGE_PATHS)) {
      const dir = path.join(APP, group);
      const provides = layouts.filter(
        (l) => l.f.startsWith(dir + path.sep) && l.src.includes(`group="${group}"`)
      );
      expect(provides.length, `no layout provides group "${group}"`).toBeGreaterThan(0);
    }
  });

  it('no layout names a group that does not exist (it would throw at render)', () => {
    for (const { f, src } of layouts) {
      for (const m of src.matchAll(/<ClientMessages[^>]*\sgroup="([^"]+)"/g)) {
        expect(Object.keys(GROUP_MESSAGE_PATHS), f).toContain(m[1]);
      }
    }
  });

  it('the root layout provides BASE only, and nothing else instantiates NextIntlClientProvider', () => {
    const root = fs.readFileSync(path.join(APP, 'layout.tsx'), 'utf8');
    expect(root).toMatch(/<ClientMessages locale=\{locale\}>/);
    expect(root).not.toContain('getMessages');
    for (const { f, src } of layouts) expect(src, f).not.toContain('NextIntlClientProvider');
  });
});

describe('the point of it: ordinary pages carry almost no messages', () => {
  const bytes = (paths: readonly string[]) => {
    const dir = path.join(WEB, 'messages', 'zh-CN');
    let n = 0;
    for (const p of new Set(paths)) {
      const [file, top] = p.split('.');
      const json = JSON.parse(fs.readFileSync(path.join(dir, `${file}.json`), 'utf8'));
      n += JSON.stringify(top === undefined ? json : json[top]).length;
    }
    return n;
  };
  const all = fs
    .readdirSync(path.join(WEB, 'messages', 'zh-CN'))
    .reduce((n, f) => n + JSON.stringify(JSON.parse(fs.readFileSync(path.join(WEB, 'messages', 'zh-CN', f), 'utf8'))).length, 0);

  it('BASE is a small fraction of all messages', () => {
    expect(bytes(BASE_MESSAGE_PATHS)).toBeLessThan(all * 0.1);
  });

  it('no group comes close to shipping everything', () => {
    for (const [g, extra] of Object.entries(GROUP_MESSAGE_PATHS)) {
      expect(bytes([...BASE_MESSAGE_PATHS, ...extra]), g).toBeLessThan(all * 0.5);
    }
  });
});
