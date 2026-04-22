import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { BrandProvider } from '@/components/providers/BrandProvider';
import { KAITU, OVERLEAP } from '@/lib/brands';
import { useCurrentBrand } from '../useCurrentBrand';

describe('useCurrentBrand', () => {
  it('returns "kaitu" when rendered inside <BrandProvider brand={KAITU}>', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <BrandProvider brand={KAITU}>{children}</BrandProvider>
    );
    const { result } = renderHook(() => useCurrentBrand(), { wrapper });
    expect(result.current).toBe('kaitu');
  });

  it('returns "overleap" when rendered inside <BrandProvider brand={OVERLEAP}>', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <BrandProvider brand={OVERLEAP}>{children}</BrandProvider>
    );
    const { result } = renderHook(() => useCurrentBrand(), { wrapper });
    expect(result.current).toBe('overleap');
  });

  it('returns "kaitu" (default) when rendered without any BrandProvider wrapper', () => {
    // createContext's default value is KAITU, so unwrapped consumers read kaitu.
    const { result } = renderHook(() => useCurrentBrand());
    expect(result.current).toBe('kaitu');
  });
});
