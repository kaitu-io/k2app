/**
 * globals.css defines the kaitu palette on :root. The overleap deployment
 * overrides it under html[data-brand="overleap"] (the attribute is set by
 * src/app/[locale]/layout.tsx). A variable missing from the override block
 * silently renders in kaitu green on overleap.io — so the override must cover
 * EVERY variable :root declares, and must actually differ on the identity ones.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

const css = fs.readFileSync(path.resolve(__dirname, '../src/app/globals.css'), 'utf8');

function block(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  expect(start, `${selector} block not found`).toBeGreaterThanOrEqual(0);
  const end = css.indexOf('\n}', start);
  return css.slice(start, end);
}

function vars(body: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const m of body.matchAll(/^\s*(--[a-z0-9-]+):\s*([^;]+);/gim)) out.set(m[1], m[2].trim());
  return out;
}

const root = vars(block(':root'));
const overleap = vars(block('html[data-brand="overleap"]'));

describe('overleap theme override', () => {
  it(':root declares the kaitu palette (liveness)', () => {
    expect(root.size).toBeGreaterThan(20);
    expect(root.get('--primary')).toBe('#00ff88');
  });

  it('overrides every :root variable — none may leak through', () => {
    const missing = [...root.keys()].filter((k) => !overleap.has(k));
    expect(missing).toEqual([]);
  });

  it('declares nothing :root does not (no orphan variables)', () => {
    const extra = [...overleap.keys()].filter((k) => !root.has(k));
    expect(extra).toEqual([]);
  });

  it('identity variables actually differ from kaitu', () => {
    for (const k of ['--background', '--card', '--primary', '--secondary', '--border', '--ring', '--radius', '--sidebar-primary', '--chart-1']) {
      expect(overleap.get(k), k).not.toBe(root.get(k));
    }
    expect(overleap.get('--primary')).toBe('#7C5CFF');
    expect(overleap.get('--radius')).toBe('0.75rem');
  });

  it('does not touch --font-mono (would break /k2 code blocks)', () => {
    expect(block('html[data-brand="overleap"]')).not.toMatch(/--font-mono/);
  });
});
