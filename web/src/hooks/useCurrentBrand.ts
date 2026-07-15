'use client';

import { siteBrand, type BrandId } from '@/lib/brands';

/** Baked brand id ('kaitu' | 'overleap'). No context — NEXT_PUBLIC_BRAND is build-time. */
export function useCurrentBrand(): BrandId {
  return siteBrand().id;
}
