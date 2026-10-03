import { describe, it, expect, vi } from 'vitest';

// Stub `server-only` — it's a side-effect module that errors when imported in
// non-RSC contexts (same pattern as request-pathname.test.ts).
vi.mock('server-only', () => ({}));

import { getBrand } from '../brand-server';
import { KAITU } from '../brands';

describe('getBrand', () => {
  it('returns the site brand regardless of locale argument', async () => {
    expect(await getBrand()).toBe(KAITU);
    expect(await getBrand('zh-TW')).toBe(KAITU);
  });
});
