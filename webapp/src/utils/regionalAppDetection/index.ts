/**
 * Region-aware AppDetector dispatcher.
 *
 * The country signal comes from `useConfigStore.country` — the value the
 * user (or auto-detect) set in 高级设置 → 智能分流 country picker. It is a
 * lowercase ISO 3166-1 alpha-2 code (e.g. 'cn'). The dispatcher normalises
 * with `toLowerCase()` to defend against any external source returning
 * upper-case.
 *
 * Adding a new region: write `<region>.ts` exporting an `AppDetector`, then
 * add one line to `REGISTRY`. The store and UI stay untouched.
 */
import { chinaDetector } from './china';
import { noopDetector } from './noop';
import type { AppDetector } from './types';

const REGISTRY: Readonly<Record<string, AppDetector>> = {
  cn: chinaDetector,
};

export function getRegionalDetector(country: string | null | undefined): AppDetector {
  if (!country) return noopDetector;
  return REGISTRY[country.toLowerCase()] ?? noopDetector;
}

export { chinaDetector } from './china';
export { noopDetector } from './noop';
export type { AppDetector, AutoDetectedAppEntry } from './types';
