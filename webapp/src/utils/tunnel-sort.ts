/**
 * Tunnel Sorting Utilities
 *
 * Sort tunnels by recommendation (route quality), with country as secondary sort.
 */

import type { Tunnel } from '../services/api-types';

/**
 * Interface for route quality lookup.
 */
export interface RouteQualityProvider {
  getRouteQuality: (domain: string) => number;
}

/**
 * Sort tunnels by recommendation (route quality).
 * Higher quality tunnels appear first. If quality is equal, sort by country alphabetically.
 *
 * @param tunnels - Array of tunnels to sort
 * @param qualityProvider - Object with getRouteQuality function
 * @returns New sorted array (does not mutate original)
 */
export function sortTunnelsByRecommendation(
  tunnels: Tunnel[],
  qualityProvider: RouteQualityProvider
): Tunnel[] {
  return [...tunnels].sort((a, b) => {
    const qualityA = qualityProvider.getRouteQuality(a.domain.toLowerCase());
    const qualityB = qualityProvider.getRouteQuality(b.domain.toLowerCase());

    // Higher quality first
    if (qualityB !== qualityA) {
      return qualityB - qualityA;
    }

    // If same quality, sort by country alphabetically
    return a.node.country.localeCompare(b.node.country);
  });
}
