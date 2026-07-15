'use client';

import { siteBrand, type Brand } from '@/lib/brands';

/**
 * The baked deployment brand, for client components.
 *
 * Phase 2 bakes the brand at build time: NEXT_PUBLIC_BRAND is inlined into the
 * client bundle, so there is no context to read and no provider to install —
 * this is a thin, stable call site over siteBrand() rather than a stateful hook.
 */
export function useBrand(): Brand {
  return siteBrand();
}
