import type { Tunnel } from '../services/api-types';

export interface CountryEntry {
  /** lowercase ISO 3166-1 alpha-2 */
  code: string;
  /** number of tunnels in this country */
  count: number;
}

/**
 * Derive the unique country list from a tunnel list, sorted by tunnel count
 * desc. Shared by K2subConfig (gateway country picker) and
 * CountryFilterDialog (auto-pick exclusion filter).
 */
export function buildCountryList(tunnels: Tunnel[]): CountryEntry[] {
  const counts: Record<string, number> = {};
  for (const t of tunnels) {
    const code = (t.node?.country ?? '').toLowerCase();
    if (code) counts[code] = (counts[code] ?? 0) + 1;
  }
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1])
    .map(([code, count]) => ({ code, count }));
}
