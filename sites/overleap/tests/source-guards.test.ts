/**
 * Structural guards for a single-brand site.
 *  - No text of the other brand or its China-market payment channels anywhere we ship.
 *  - No imports reaching into ../../web (this app shares protocol contracts, not code).
 *  - Only this site's locales exist under messages/.
 */
import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'fs';
import path from 'path';
import { LOCALES } from '@/lib/site';

const ROOT = path.resolve(__dirname, '..');
const SCAN_DIRS = ['src', 'messages', 'public/legal'];
const TEXT_EXT = /\.(tsx?|json|md|css)$/;

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = path.join(dir, name);
    return statSync(p).isDirectory() ? files(p) : TEXT_EXT.test(p) ? [p] : [];
  });
}
const all = SCAN_DIRS.flatMap((d) => files(path.join(ROOT, d)));

describe('source guards', () => {
  it('scans a non-trivial set of files', () => {
    expect(all.length).toBeGreaterThan(40);
  });

  // Built from parts so this file does not trip its own scan.
  const FORBIDDEN = [['开', '途'], ['Kai', 'tu'], ['kai', 'tu.io'], ['支付', '宝'], ['微信', '支付'], ['银', '联'], ['Word', 'Gate']].map((p) => p.join(''));

  it.each(FORBIDDEN)('no "%s" in shipped sources', (word) => {
    const hits = all.filter((f) => f !== __filename && readFileSync(f, 'utf8').includes(word));
    expect(hits.map((f) => path.relative(ROOT, f))).toEqual([]);
  });

  // The manager has exactly one entry (the other site's /manager, backed by /app/*).
  // This site never grows an admin surface: no route segment named manager / admin,
  // and the middleware 404s /app/* (tests/middleware.test.ts).
  it('has no admin route tree', () => {
    const dirs = (dir: string): string[] =>
      readdirSync(dir).flatMap((name) => {
        const p = path.join(dir, name);
        return statSync(p).isDirectory() ? [p, ...dirs(p)] : [];
      });
    const hits = dirs(path.join(ROOT, 'src/app')).filter((d) =>
      /^\(?(manager|admin)\)?$/i.test(path.basename(d)),
    );
    expect(hits.map((d) => path.relative(ROOT, d))).toEqual([]);
  });

  it('no imports from the web/ app', () => {
    const hits = all.filter((f) => /from ['"](\.\.\/)+web\//.test(readFileSync(f, 'utf8')));
    expect(hits).toEqual([]);
  });

  it.each(['messages'])('%s/ holds only this site’s locales', (dir) => {
    const dirs = readdirSync(path.join(ROOT, dir)).filter((n) => statSync(path.join(ROOT, dir, n)).isDirectory());
    expect(dirs.sort()).toEqual([...LOCALES].sort());
  });
});
