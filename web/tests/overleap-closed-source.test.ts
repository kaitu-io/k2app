/**
 * Neither site links a source-code host (2026-09-30): overleap is positioned
 * as a closed, self-contained product, and kaitu has no public code org of its
 * own for now. That covers the header/footer, the JSON-LD `sameAs` that search
 * engines read, and the velite docs (e.g. a `git clone` of the k2 repo).
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const WEB = join(__dirname, '..');

function walk(dir: string, keep: (p: string) => boolean, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, keep, out);
    else if (keep(p)) out.push(p);
  }
  return out;
}

describe('overleap sources never link a code host', () => {
  const files = walk(join(WEB, 'src'), (p) => /\.overleap\.tsx?$/.test(p) || p.endsWith(join('lib', 'site', 'overleap.ts')));

  it('finds the overleap page tree', () => {
    expect(files.length).toBeGreaterThan(3);
  });

  it.each(files)('%s', (file) => {
    expect(readFileSync(file, 'utf8')).not.toMatch(/github\.com|gitlab\.com/);
  });
});

describe('no site links our own GitHub org', () => {
  const files = [
    ...walk(join(WEB, 'src'), (p) => /\.(tsx?|json)$/.test(p) && !p.includes('__tests__')),
    ...walk(join(WEB, 'content'), (p) => /\.mdx?$/.test(p)),
    ...walk(join(WEB, 'messages'), (p) => p.endsWith('.json')),
  ];

  it('scans src, content and messages', () => {
    expect(files.length).toBeGreaterThan(100);
  });

  it('none of them mention github.com/getoverleap', () => {
    const hits = files.filter((f) => /github\.com\/getoverleap/i.test(readFileSync(f, 'utf8')));
    expect(hits).toEqual([]);
  });
});

// k2 ships as binaries only (install script / downloads) — the docs must not
// tell readers to clone, build or run anything from a source tree, nor claim
// that any part of it is open source.
describe('k2 docs assume binaries only, no source', () => {
  const docs = walk(join(WEB, 'content'), (p) => /\/k2\/[^/]+\.mdx?$/.test(p));
  const SOURCE_ONLY = /git clone|cd k2\/|docker compose up --build|已开源|开源在|is open source —|(framework|suite) is open source|Docker 镜像|Docker images?/i;

  it('finds the k2 docs', () => {
    expect(docs.length).toBeGreaterThan(10);
  });

  it.each(docs)('%s', (file) => {
    expect(readFileSync(file, 'utf8')).not.toMatch(SOURCE_ONLY);
  });
});
