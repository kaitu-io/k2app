/**
 * Tunnel Sorting Utilities
 *
 * Sort tunnels by recommendation (route quality), with load as secondary sort.
 */

import type { Tunnel } from '../services/api-types';

/**
 * Interface for route quality lookup.
 * Supports both evaluation store (uses getRouteQuality function) and
 * legacy diagnosis store (uses Map with DiagnoseNodeResult).
 */
export interface RouteQualityProvider {
  getRouteQuality: (domain: string) => number;
}

/**
 * Sort tunnels by recommendation (route quality).
 * Higher quality tunnels appear first. If quality is equal, lower load first.
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

    // If same quality, lower load first
    return (a.node.load ?? 100) - (b.node.load ?? 100);
  });
}
