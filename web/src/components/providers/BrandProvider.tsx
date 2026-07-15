'use client';

import { type ReactNode } from 'react';
import { siteBrand, type Brand } from '@/lib/brands';

/**
 * Phase 2: the brand is baked into the bundle (NEXT_PUBLIC_BRAND is inlined at
 * build time), so no React context is needed. BrandProvider is kept as a
 * pass-through for API stability; useBrand() reads the baked registry entry.
 */
export function BrandProvider({ children }: { brand?: Brand; children: ReactNode }) {
  return children;
}

export function useBrand(): Brand {
  return siteBrand();
}
