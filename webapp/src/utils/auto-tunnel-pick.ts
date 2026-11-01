import type { Tunnel } from '../services/api-types';

/** Cached probe outcome for one tunnel. Only fresh, measured results are passed in. */
export type TunnelProbe = { avgRttMs: number; reachable: boolean };

/** Minimum number of probed nodes before the line gate applies. */
const NEAR_GATE_MIN_PROBED = 3;
const NEAR_GATE_RATIO = 1.5;
const NEAR_GATE_SLACK_MS = 30;

/**
 * Pick one tunnel for Auto mode in three steps:
 *
 * 1. Candidates: `recommendScore > 0` (hard blacklist, NO fallback — see
 *    api/CLAUDE.md "Tunnel Scoring"), has `serverUrl`, `node.country` not in
 *    `excludedCountries` (NO fallback — the user excluded it on purpose).
 *    Nodes whose fresh probe says `reachable=false` are also dropped, unless
 *    that would empty the pool (then the unreachable filter is ignored).
 * 2. Line gate: when >= 3 candidates have probe data, keep only those with
 *    `avgRttMs <= max(best * 1.5, best + 30ms)`; unprobed ones are dropped
 *    too. With fewer than 3 probed nodes the gate is skipped.
 * 3. Power of two choices: draw two DISTINCT candidates at random and take the
 *    higher `recommendScore` (tie keeps the first drawn). One candidate is
 *    returned directly.
 *
 * Returns null when the pool is empty — caller MUST surface
 * NO_TUNNEL_AVAILABLE_AUTO (or NO_TUNNEL_AVAILABLE_FILTERED when the emptiness
 * is caused by the country filter).
 *
 * @param excludedCountries ISO 3166-1 alpha-2 codes to skip (any case)
 * @param rng injectable randomness for tests; defaults to Math.random
 * @param probeOf cached probe lookup; null = no usable data (absent => none)
 */
export function pickAutoTunnel(
  tunnels: Tunnel[],
  excludedCountries: readonly string[] = [],
  rng: () => number = Math.random,
  probeOf: (t: Tunnel) => TunnelProbe | null = () => null,
): Tunnel | null {
  if (tunnels.length === 0) return null;
  const excluded = new Set(excludedCountries.map(c => c.toLowerCase()));
  let pool = tunnels.filter(t =>
    t.recommendScore > 0
    && !!t.serverUrl
    && !excluded.has((t.node?.country ?? '').toLowerCase()),
  );
  if (pool.length === 0) return null;

  const reachablePool = pool.filter(t => probeOf(t)?.reachable !== false);
  if (reachablePool.length > 0) pool = reachablePool;

  const rtts = new Map<Tunnel, number>();
  for (const t of pool) {
    const p = probeOf(t);
    if (p) rtts.set(t, p.avgRttMs);
  }
  if (rtts.size >= NEAR_GATE_MIN_PROBED) {
    const best = Math.min(...rtts.values());
    const limit = Math.max(best * NEAR_GATE_RATIO, best + NEAR_GATE_SLACK_MS);
    pool = pool.filter(t => {
      const rtt = rtts.get(t);
      return rtt !== undefined && rtt <= limit;
    });
  }

  if (pool.length === 1) return pool[0];
  const n = pool.length;
  const i = Math.min(Math.floor(rng() * n), n - 1);
  // Second index drawn from the n-1 others and shifted past i: no rejection loop.
  let j = Math.min(Math.floor(rng() * (n - 1)), n - 2);
  if (j >= i) j++;
  const first = pool[i];
  const second = pool[j];
  return second.recommendScore > first.recommendScore ? second : first;
}
