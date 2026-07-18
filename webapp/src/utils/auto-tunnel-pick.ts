import type { Tunnel } from '../services/api-types';

const AUTO_PICK_TOP_N = 5;

/**
 * Pick one tunnel from the top-N (default 5) by recommendScore at random.
 *
 * - Excludes tunnels with `recommendScore === 0` (project hard-blacklist
 *   convention, see api/CLAUDE.md "Tunnel Scoring") with NO fallback —
 *   a blacklisted tunnel is never picked even if it's the only candidate.
 * - Excludes tunnels missing `serverUrl` (downstream `_k2.run('up')` would crash).
 * - Excludes tunnels whose `node.country` is in `excludedCountries`
 *   (user preference, case-insensitive) with NO fallback — the user excluded
 *   the country because it does not work for them; silently reconnecting
 *   through it would be worse than failing loudly.
 * - Returns null when the pool is empty — caller MUST surface
 *   NO_TUNNEL_AVAILABLE_AUTO (or NO_TUNNEL_AVAILABLE_FILTERED when the
 *   emptiness is caused by the country filter) to the user.
 *
 * Stable score-desc sort: ties keep input order (database / country-sorted order).
 *
 * @param excludedCountries ISO 3166-1 alpha-2 codes to skip (any case)
 * @param rng injectable randomness for tests; defaults to Math.random
 */
export function pickAutoTunnel(
  tunnels: Tunnel[],
  excludedCountries: readonly string[] = [],
  rng: () => number = Math.random,
): Tunnel | null {
  if (tunnels.length === 0) return null;
  const excluded = new Set(excludedCountries.map(c => c.toLowerCase()));
  const pool = tunnels.filter(t =>
    t.recommendScore > 0
    && !!t.serverUrl
    && !excluded.has((t.node?.country ?? '').toLowerCase()),
  );
  if (pool.length === 0) return null;
  const sorted = [...pool].sort((a, b) => b.recommendScore - a.recommendScore);
  const top = sorted.slice(0, AUTO_PICK_TOP_N);
  return top[Math.floor(rng() * top.length)];
}
