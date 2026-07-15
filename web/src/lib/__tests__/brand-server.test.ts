import { describe, it, expect, vi, afterEach } from 'vitest';

// Stub `server-only` — it's a side-effect module that errors when imported in
// non-RSC contexts (same pattern as request-pathname.test.ts).
vi.mock('server-only', () => ({}));

// brand-server used to import next/headers — the rewrite must NOT anymore.
import { getBrand } from '../brand-server';

describe('getBrand (baked)', () => {
  afterEach(() => vi.unstubAllEnvs());

  it('returns the baked brand regardless of locale argument', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect((await getBrand()).id).toBe('overleap');
    expect((await getBrand('zh-CN')).id).toBe('overleap'); // locale no longer influences brand
  });

  it('defaults to kaitu', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', '');
    expect((await getBrand()).id).toBe('kaitu');
  });
});
