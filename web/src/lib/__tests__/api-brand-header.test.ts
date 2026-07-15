import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { api } from '../api';

describe('api client X-K2-Brand header', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ code: 0, data: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it('sends X-K2-Brand: kaitu by default', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', '');
    await api.getPlans(); // any public GET endpoint on the api object
    const [, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(new Headers(init.headers).get('X-K2-Brand')).toBe('kaitu');
  });

  it('sends X-K2-Brand: overleap when baked', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    await api.getPlans();
    const [, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(new Headers(init.headers).get('X-K2-Brand')).toBe('overleap');
  });
});
