/**
 * Binary brand-asset guard.
 *
 * Text guards (brand-guard / brand-leak-ssr) cannot see images. On 2026-09-04
 * public/overleap-icon.png and public/brand/overleap/* were byte-for-byte the
 * kaitu K2 icon and public/overleap-og.png was the kaitu OG poster — a leak no
 * regex could catch. This test pins: every Overleap asset exists, is non-empty,
 * and shares no SHA-256 with any kaitu asset.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { KAITU, OVERLEAP } from '../src/lib/brands';

const PUBLIC = path.resolve(__dirname, '../public');

function sha256(rel: string): string {
  return crypto.createHash('sha256').update(fs.readFileSync(path.join(PUBLIC, rel))).digest('hex');
}

const FAVICON_FILES = [
  'favicon-16x16.png', 'favicon-32x32.png', 'icon-48x48.png',
  'icon-96x96.png', 'icon-192x192.png', 'icon-512x512.png',
];

/** kaitu's favicon set lives at the public root (faviconPrefix ''). */
function faviconPath(prefix: string, file: string): string {
  return `${prefix}/${file}`;
}

const OVERLEAP_ASSETS = [
  OVERLEAP.logoPath,
  OVERLEAP.ogImagePath,
  ...FAVICON_FILES.map((f) => faviconPath(OVERLEAP.faviconPrefix, f)),
];

const KAITU_ASSETS = [
  KAITU.logoPath,
  KAITU.ogImagePath,
  ...FAVICON_FILES.map((f) => faviconPath(KAITU.faviconPrefix, f)),
];

describe('overleap binary assets', () => {
  it('registry paths under test are non-empty (liveness)', () => {
    expect(OVERLEAP_ASSETS.length).toBeGreaterThan(2);
    expect(KAITU_ASSETS.length).toBeGreaterThan(2);
  });

  it.each(OVERLEAP_ASSETS)('%s exists and is non-empty', (rel) => {
    const p = path.join(PUBLIC, rel);
    expect(fs.existsSync(p), `${rel} missing`).toBe(true);
    expect(fs.statSync(p).size).toBeGreaterThan(0);
  });

  it('shares no file hash with any kaitu asset', () => {
    const kaituHashes = new Set(
      KAITU_ASSETS.filter((rel) => fs.existsSync(path.join(PUBLIC, rel))).map(sha256),
    );
    expect(kaituHashes.size).toBeGreaterThan(0);
    const leaks = OVERLEAP_ASSETS.filter(
      (rel) => fs.existsSync(path.join(PUBLIC, rel)) && kaituHashes.has(sha256(rel)),
    );
    expect(leaks, `overleap assets identical to kaitu: ${leaks.join(', ')}`).toEqual([]);
  });
});
