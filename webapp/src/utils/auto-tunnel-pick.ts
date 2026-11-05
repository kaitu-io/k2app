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
 * Pick one tunnel for Auto mode: top 80% by score, then power of two choices
 * (uniform when at most two remain).
 *
 * 1. Candidates: `recommendScore > 0` (hard blacklist, NO fallback — see
 *    api/CLAUDE.md "Tunnel Scoring"), has `serverUrl`, `node.country` not in
 *    `excludedCountries` (NO fallback — the user excluded it on purpose).
 * 2. Top 80%: shuffle, stable sort by score descending, keep ceil(n * 4/5).
 *    The shuffle comes first so ties at the cut do not depend on API order.
 * 3. One or two kept candidates: uniform pick. P2C on two always returns the
 *    higher score, so a retry could never leave a node blocked on the user's
 *    network.
 * 4. Three or more: power of two choices. Draw two DISTINCT candidates at random
 *    and take the higher `recommendScore` (tie keeps the first drawn).
 *
 * Consequence of step 4: with distinct scores the lowest kept candidate loses
 * every comparison, so it is never returned.
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
  if (n <= 2) return pool[randomIndex(rng, n)];
  const i = randomIndex(rng, n);
  // Second index drawn from the n-1 others and shifted past i: no rejection loop.
  let j = randomIndex(rng, n - 1);
  if (j >= i) j++;
  const first = pool[i];
  const second = pool[j];
  return second.recommendScore > first.recommendScore ? second : first;
}

/**
 * Machine identity for de-duplication: node ipv4, else ipv6. A tunnel with
 * neither counts as its own machine (null = never collides).
 *
 * Several tunnels can live on one machine; a fallback on the same machine
 * fails together with the primary, so it buys nothing.
 */
function machineKey(t: Tunnel): string | null {
  return t.node?.ipv4 || t.node?.ipv6 || null;
}

/**
 * Pick the Auto primary plus fallbacks, each on a different machine.
 *
 * The first element is exactly what `pickAutoTunnel` returns for the same
 * arguments and rng, so the primary's distribution is unchanged. Each later
 * element is another `pickAutoTunnel` draw (same rng) from the pool with every
 * tunnel on an already-chosen machine removed. Stops at `count` or when the
 * pool runs dry; `[]` when there is no eligible tunnel at all.
 *
 * @param count total tunnels to return, primary included (default 4)
 */
export function pickAutoCandidates(
  tunnels: Tunnel[],
  excludedCountries: readonly string[] = [],
  count = 4,
  rng: () => number = Math.random,
): Tunnel[] {
  const picked: Tunnel[] = [];
  let pool = tunnels;
  while (picked.length < count) {
    const t = pickAutoTunnel(pool, excludedCountries, rng);
    if (!t) break;
    picked.push(t);
    const key = machineKey(t);
    pool = pool.filter(x => x !== t && (key === null || machineKey(x) !== key));
  }
  return picked;
}
