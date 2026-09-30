/**
 * Brand params shared by admin tools.
 *
 * Center /app/* is the cross-brand admin surface: it manages both brands' data.
 * - Creates of brand-owned entities (plan / campaign / announcement / license key
 *   batch / EDM template) must name the owning brand — the API rejects an empty one
 *   instead of silently defaulting.
 * - List / stat endpoints accept `?brand=`; omitted = all brands.
 * Brand ids mirror api/brand.go brandRegistry.
 */

import { z } from 'zod'

export const BRAND_IDS = ['kaitu', 'overleap'] as const

export const brandRequired = z.enum(BRAND_IDS).describe('Owning brand (required; immutable after create)')

export const brandFilter = z
  .enum(BRAND_IDS)
  .optional()
  .describe('Filter by brand; omit = all brands')

/** Adds `brand` to a query record when set. */
export function withBrand(q: Record<string, string>, brand: unknown): Record<string, string> {
  if (brand !== undefined && brand !== '') q.brand = String(brand)
  return q
}
