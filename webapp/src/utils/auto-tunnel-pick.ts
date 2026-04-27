import type { Tunnel } from '../services/api-types';

/**
 * Pick one tunnel from the top-3 by recommendScore at random.
 *
 * - Excludes tunnels with `recommendScore === 0` (project hard-blacklist
 *   convention, see api/CLAUDE.md "Tunnel Scoring").
 * - Excludes tunnels missing `serverUrl` (downstream `_k2.run('up')` would crash).
 * - Falls back to non-empty `serverUrl` pool when zero-filtered pool is empty.
 * - Returns null when nothing has a usable serverUrl — caller MUST surface a user error.
 *
 * Stable score-desc sort: ties keep input order (database / country-sorted order).
 *
 * @param rng injectable randomness for tests; defaults to Math.random
 */
export function pickAutoTunnel(
  tunnels: Tunnel[],
  rng: () => number = Math.random,
): Tunnel | null {
  if (tunnels.length === 0) return null;
  const eligible = tunnels.filter(t => t.recommendScore > 0 && !!t.serverUrl);
  const pool = eligible.length > 0 ? eligible : tunnels.filter(t => !!t.serverUrl);
  if (pool.length === 0) return null;
  const sorted = [...pool].sort((a, b) => b.recommendScore - a.recommendScore);
  const top = sorted.slice(0, 3);
  return top[Math.floor(rng() * top.length)];
}
