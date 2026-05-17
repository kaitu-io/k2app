import type { Tunnel } from '../services/api-types';

const AUTO_PICK_TOP_N = 5;

/**
 * Pick one tunnel from the top-N (default 5) by recommendScore at random.
 *
 * - Excludes tunnels with `recommendScore === 0` (project hard-blacklist
 *   convention, see api/CLAUDE.md "Tunnel Scoring") with NO fallback —
 *   a blacklisted tunnel is never picked even if it's the only candidate.
 * - Excludes tunnels missing `serverUrl` (downstream `_k2.run('up')` would crash).
 * - Returns null when no tunnel has score>0 — caller MUST surface
 *   NO_TUNNEL_AVAILABLE_AUTO to the user.
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
  const pool = tunnels.filter(t => t.recommendScore > 0 && !!t.serverUrl);
  if (pool.length === 0) return null;
  const sorted = [...pool].sort((a, b) => b.recommendScore - a.recommendScore);
  const top = sorted.slice(0, AUTO_PICK_TOP_N);
  return top[Math.floor(rng() * top.length)];
}
