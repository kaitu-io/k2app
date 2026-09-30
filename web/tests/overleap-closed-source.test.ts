/**
 * Overleap is positioned as a closed, self-contained product (2026-09-30):
 * nothing rendered for overleap.io may point at a source-code host — not the
 * footer, and not the JSON-LD `sameAs` that search engines read.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

function overleapSources(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) overleapSources(p, out);
    else if (/\.overleap\.tsx?$/.test(name) || p.endsWith(join('lib', 'site', 'overleap.ts'))) out.push(p);
  }
  return out;
}

describe('overleap sources never link a code host', () => {
  const files = overleapSources(join(__dirname, '..', 'src'));

  it('finds the overleap page tree', () => {
    expect(files.length).toBeGreaterThan(3);
  });

  it.each(files)('%s', (file) => {
    expect(readFileSync(file, 'utf8')).not.toMatch(/github\.com|gitlab\.com/);
  });
});
