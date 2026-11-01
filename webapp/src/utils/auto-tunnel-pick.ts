import type { Tunnel } from '../services/api-types';

/** Share of candidates kept by score, as a ratio of integers: 4/5 = 80%. */
const KEEP_NUMERATOR = 4;
const KEEP_DENOMINATOR = 5;

/** Index in [0, n). Tolerates an rng that returns 1, a negative number or NaN. */
function randomIndex(rng: () => number, n: number): number {
  const i = Math.floor(rng() * n);
  return i >= 0 ? Math.min(i, n - 1) : 0;
}

/**
 * Pick one tunnel for Auto mode: top 80% by score, then power of two choices.
 *
 * 1. Candidates: `recommendScore > 0` (hard blacklist, NO fallback — see
 *    api/CLAUDE.md "Tunnel Scoring"), has `serverUrl`, `node.country` not in
 *    `excludedCountries` (NO fallback — the user excluded it on purpose).
 * 2. Top 80%: shuffle, stable sort by score descending, keep ceil(n * 4/5).
 *    The shuffle comes first so ties at the cut do not depend on API order.
 * 3. Power of two choices: draw two DISTINCT candidates at random and take the
 *    higher `recommendScore` (tie keeps the first drawn). A single remaining
 *    candidate is returned directly.
 *
 * Consequence of step 3: with distinct scores the lowest kept candidate loses
 * every comparison, so it is never returned; two kept candidates always resolve
 * to the higher one.
 *
 * There is NO line or geo awareness here. `recommendScore` only measures a
 * node's traffic budget (api/logic_tunnel_score.go), not its distance from the
 * user, and the client has no measurement to add: `runProbe()` has no production
 * caller and mobile has no probe capability, so this function must not read the
 * probe store.
 *
 * Returns null when the pool is empty — caller MUST surface
 * NO_TUNNEL_AVAILABLE_AUTO (or NO_TUNNEL_AVAILABLE_FILTERED when the emptiness
 * is caused by the country filter).
 *
 * @param excludedCountries ISO 3166-1 alpha-2 codes to skip (any case)
 * @param rng injectable randomness for tests; defaults to Math.random
 */
export function pickAutoTunnel(
  tunnels: Tunnel[],
  excludedCountries: readonly string[] = [],
  rng: () => number = Math.random,
): Tunnel | null {
  const excluded = new Set(excludedCountries.map(c => c.toLowerCase()));
  const candidates = tunnels.filter(t =>
    t.recommendScore > 0
    && !!t.serverUrl
    && !excluded.has((t.node?.country ?? '').toLowerCase()),
  );
  if (candidates.length === 0) return null;

  // Fisher-Yates on the filtered copy; the caller's array is untouched.
  for (let k = candidates.length - 1; k > 0; k--) {
    const r = randomIndex(rng, k + 1);
    [candidates[k], candidates[r]] = [candidates[r], candidates[k]];
  }
  candidates.sort((a, b) => b.recommendScore - a.recommendScore);

  // Integer product before the division; at least 1 because candidates is non-empty.
  const keep = Math.ceil((candidates.length * KEEP_NUMERATOR) / KEEP_DENOMINATOR);
  const pool = candidates.slice(0, keep);

  const n = pool.length;
  if (n === 1) return pool[0];
  const i = randomIndex(rng, n);
  // Second index drawn from the n-1 others and shifted past i: no rejection loop.
  let j = randomIndex(rng, n - 1);
  if (j >= i) j++;
  const first = pool[i];
  const second = pool[j];
  return second.recommendScore > first.recommendScore ? second : first;
}
