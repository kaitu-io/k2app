/**
 * Active brand resolution — build-time constant.
 *
 * __K2_BRAND__ is a Vite/Vitest define fed by env K2_BRAND (default 'kaitu').
 * The ternary below constant-folds after define substitution, so the inactive
 * brand's module is tree-shaken out of production bundles (verified by
 * scripts/check-brand-purity.sh).
 */
import { KAITU_BRAND } from './kaitu';
import { OVERLEAP_BRAND } from './overleap';
import type { BrandId, WebappBrandConfig } from './types';

// Same pattern as __K2_BUILD_LOG_LEVEL__ in stores/config.store.ts.
declare const __K2_BRAND__: string;

export const brandConfig: WebappBrandConfig =
  __K2_BRAND__ === 'overleap' ? OVERLEAP_BRAND : KAITU_BRAND;

export function getBrandId(): BrandId {
  return brandConfig.id;
}

export type { BrandId, WebappBrandConfig, BrandFeatures, BrandThemeTokens } from './types';
