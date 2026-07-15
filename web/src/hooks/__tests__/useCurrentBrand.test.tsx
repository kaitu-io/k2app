import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useCurrentBrand } from '../useCurrentBrand';

afterEach(() => vi.unstubAllEnvs());

describe('useCurrentBrand (baked)', () => {
  it('returns kaitu by default', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', '');
    expect(renderHook(() => useCurrentBrand()).result.current).toBe('kaitu');
  });
  it('returns overleap when baked', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect(renderHook(() => useCurrentBrand()).result.current).toBe('overleap');
  });
});
