'use client';

import { useBrand } from '@/components/providers/BrandProvider';
import type { BrandId } from '@/lib/brands';

/**
 * Thin client-side accessor that returns the current brand's id (`'kaitu'` | `'overleap'`).
 *
 * Backed by the React context set in `[locale]/layout.tsx` (`<BrandProvider brand={...}>`),
 * which derives the brand on the server from the request host. Using the context — rather
 * than reading `document.documentElement.dataset.brand` — avoids SSR/hydration mismatch.
 *
 * The `<html data-brand>` attribute injected in the same layout remains available for
 * CSS selectors, theming, and debugging, but is not this hook's data source.
 */
export function useCurrentBrand(): BrandId {
  return useBrand().id;
}
