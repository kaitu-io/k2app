'use client';

import { siteBrand, type Brand } from '@/lib/brands';

/**
 * The site's brand, for client components. This site serves kaitu only, so
 * there is no context to read and no provider to install — this is a thin,
 * stable call site over siteBrand() rather than a stateful hook.
 */
export function useBrand(): Brand {
  return siteBrand();
}
