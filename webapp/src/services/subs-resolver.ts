/**
 * subs-resolver — mobile-only k2subs:// → k2v5:// resolver.
 *
 * Desktop daemon resolves subscription URLs server-side via
 * `daemon.resolveSubscriptions` (k2/daemon/daemon.go:271,581). Mobile (appext)
 * has no equivalent — engine.buildOutboundMap (k2/engine/engine.go:1052) treats
 * k2subs as a reserved scheme and drops the route, so the engine fails with
 * code 570 "no k2v5 outbound configured". This module fills that gap by
 * fetching /api/subs from the main App webapp process (where HTTPS works
 * trivially) and picking a tunnel before _k2.run('up').
 *
 * NOTE: caller (`connection.store.ts`) is responsible for gating this on
 * `window._platform.platformType === 'mobile'`. Calling it on desktop would
 * be harmless but defeats the purpose — daemon does it better there.
 *
 * Wire contract (verified against k2/config/subscription.go and
 * api/api_subs.go): success = HTTP 200 + raw JSON `{tunnels:[{url,weight}],
 * refresh:N}`; error = real HTTP status + plain-text body. Center already
 * injects `udid:token@` into each k2v5 URL (api_subs.go:181), so we don't.
 */

const STORAGE_PREFIX = 'k2subs.cache.';
const FETCH_TIMEOUT_MS = 15_000;
// Cache fallback window: drop entries older than this when fetch fails. Aligned
// with access_token JWT TTL (api/config.yml:61, 86400s = 24h) — beyond that the
// cached k2v5 URLs embed a JWT that the server will reject anyway.
const STALE_FALLBACK_MAX_MS = 24 * 60 * 60 * 1000;

export interface TunnelEntry {
  url: string;
  /**
   * Legacy integer weight. New Center responses derive it as
   * round(recommendScore * 100); pre-recommendScore Center returns a static 1.
   * Kept for backward compatibility with old responses; new code should prefer
   * `recommendScore`.
   */
  weight: number;
  /**
   * Canonical recommendation signal in [0, 1], higher = better. Present on
   * Center responses post-rollout. When both fields are present, pickWeighted
   * uses this and ignores `weight`.
   */
  recommendScore?: number;
}

export interface SubsResolveResult {
  /** The picked k2v5:// URL (already includes udid:token@ from Center). */
  url: string;
  /** Full candidate set returned by Center; cached so caller can retry with exclude. */
  allCandidates: TunnelEntry[];
  /** Where the candidate set came from. */
  source: 'fresh' | 'cache';
  /** ms epoch — when the candidate set was fetched. */
  fetchedAt: number;
}

interface CacheEntry {
  tunnels: TunnelEntry[];
  refresh: number; // seconds
  fetchedAt: number; // ms epoch
}

interface SubsResponseBody {
  tunnels?: TunnelEntry[];
  refresh?: number;
}

interface ParsedSubs {
  endpoint: string;
  basicAuth: string;
  cacheKey: string;
}

/**
 * Parse a k2subs://user:pass@host/path?query URL into the upstream HTTPS form
 * + Basic auth header + cache key. Drops `refresh` from the upstream qs since
 * that's a client-side directive in the Go subscription parser
 * (k2/config/subscription.go) — but forwards everything else (e.g. country=jp).
 */
function parseSubsUrl(raw: string): ParsedSubs {
  if (!raw.startsWith('k2subs://')) {
    throw new Error('subs-resolver: scheme must be k2subs://');
  }
  // The URL constructor doesn't reliably extract userinfo for non-special
  // schemes, so swap to https for the parse and rebuild downstream.
  const httpsForm = 'https://' + raw.slice('k2subs://'.length);
  let u: URL;
  try {
    u = new URL(httpsForm);
  } catch {
    throw new Error('subs-resolver: malformed k2subs URL');
  }
  if (!u.username || !u.password) {
    throw new Error('subs-resolver: missing credentials');
  }
  const params = new URLSearchParams(u.search);
  params.delete('refresh');
  const qs = params.toString();
  const endpoint = `https://${u.host}${u.pathname}${qs ? '?' + qs : ''}`;
  const user = decodeURIComponent(u.username);
  const pass = decodeURIComponent(u.password);
  const basicAuth = 'Basic ' + btoa(user + ':' + pass);
  const cacheKey = STORAGE_PREFIX + fnv1a32hex(raw);
  return { endpoint, basicAuth, cacheKey };
}

/**
 * 32-bit FNV-1a hex digest. Non-cryptographic — only used to namespace cache
 * entries per (subscription URL, country) tuple. SubtleCrypto.digest is async
 * and overkill here.
 */
function fnv1a32hex(s: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(16).padStart(8, '0');
}

/**
 * effectiveWeight produces a single positive number per candidate for weighted
 * sampling. Prefers `recommendScore` (canonical, [0,1]) when available; falls
 * back to the legacy `weight` int. Returns 0 when neither signal is positive,
 * which downstream treats as "ineligible unless everybody is zero" (the same
 * fallback semantics as the pre-recommendScore code).
 */
function effectiveWeight(c: TunnelEntry): number {
  if (c.recommendScore !== undefined && c.recommendScore > 0) {
    return c.recommendScore;
  }
  if (c.weight > 0) {
    return c.weight;
  }
  return 0;
}

/**
 * Weighted random pick from `candidates`. Mirrors the semantics of
 * `Subscription.Pick` in k2/config/subscription.go:
 *   - candidates with a positive effective weight (recommendScore or weight)
 *     compete proportionally to that value
 *   - if nothing is positive, all are treated as weight=1 (uniform)
 *   - throws when candidates is empty (caller's job to filter exclude before)
 */
export function pickWeighted(
  candidates: TunnelEntry[],
  rng: () => number = Math.random,
): TunnelEntry {
  if (candidates.length === 0) {
    throw new Error('subs-resolver: no candidates');
  }
  let pool = candidates.filter(c => effectiveWeight(c) > 0);
  let allZero = false;
  if (pool.length === 0) {
    pool = candidates;
    allZero = true;
  }
  let total = 0;
  for (const c of pool) total += allZero ? 1 : effectiveWeight(c);
  let r = rng() * total;
  for (const c of pool) {
    r -= allZero ? 1 : effectiveWeight(c);
    if (r < 0) return c;
  }
  // Floating-point safety net.
  return pool[pool.length - 1];
}

async function readCache(cacheKey: string): Promise<CacheEntry | null> {
  try {
    const stored = await window._platform.storage.get<CacheEntry>(cacheKey);
    if (!stored || !Array.isArray(stored.tunnels)) return null;
    return stored;
  } catch (err) {
    console.warn('[SubsResolver] cache read failed:', err);
    return null;
  }
}

async function writeCache(cacheKey: string, entry: CacheEntry): Promise<void> {
  try {
    await window._platform.storage.set(cacheKey, entry);
  } catch (err) {
    console.warn('[SubsResolver] cache write failed:', err);
  }
}

async function fetchSubs(parsed: ParsedSubs): Promise<CacheEntry> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS);
  try {
    const resp = await fetch(parsed.endpoint, {
      method: 'GET',
      headers: { Authorization: parsed.basicAuth },
      signal: ctrl.signal,
    });
    if (!resp.ok) {
      const body = await resp.text().catch(() => '');
      throw new Error(`subs-resolver: HTTP ${resp.status} ${body.slice(0, 256)}`);
    }
    const body = (await resp.json()) as SubsResponseBody;
    const tunnels = (body.tunnels ?? []).filter(t => t && typeof t.url === 'string' && t.url);
    const refresh = typeof body.refresh === 'number' && body.refresh > 0 ? body.refresh : 1800;
    return { tunnels, refresh, fetchedAt: Date.now() };
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Resolve a k2subs:// URL to a single picked k2v5:// URL.
 *
 * Cache flow:
 *  1. If a cache entry exists and (now - fetchedAt) < refresh*1000 → use it
 *     ("fresh cache hit", no network).
 *  2. Otherwise fetch upstream. On success → write cache, use it.
 *  3. On fetch failure with cache present and < 24h old → use stale cache (warn).
 *  4. On fetch failure with no usable cache → throw.
 *
 * Pick flow: filter `excludeUrls` from the candidate set, then weighted random.
 */
export async function resolveTunnel(
  rawSubsUrl: string,
  excludeUrls: string[] = [],
): Promise<SubsResolveResult> {
  const parsed = parseSubsUrl(rawSubsUrl);
  const cached = await readCache(parsed.cacheKey);
  const cacheFresh = !!cached && Date.now() - cached.fetchedAt < cached.refresh * 1000;

  let entry: CacheEntry;
  let source: 'fresh' | 'cache';

  if (cacheFresh && cached) {
    entry = cached;
    source = 'cache';
    console.info(`[SubsResolver] cache hit ${parsed.cacheKey} tunnels=${cached.tunnels.length}`);
  } else {
    try {
      entry = await fetchSubs(parsed);
      source = 'fresh';
      await writeCache(parsed.cacheKey, entry);
      console.info(`[SubsResolver] fetched ${parsed.endpoint} tunnels=${entry.tunnels.length} refresh=${entry.refresh}s`);
    } catch (err) {
      if (cached && Date.now() - cached.fetchedAt < STALE_FALLBACK_MAX_MS) {
        entry = cached;
        source = 'cache';
        console.warn('[SubsResolver] fetch failed, using stale cache:', err);
      } else {
        if (cached) {
          // Cache too stale (>24h); creds inside cached k2v5 URLs are likely
          // expired anyway. Drop it so next attempt forces a fresh fetch.
          try {
            await window._platform.storage.remove(parsed.cacheKey);
          } catch {
            /* ignore */
          }
        }
        throw err;
      }
    }
  }

  if (entry.tunnels.length === 0) {
    throw new Error('subs-resolver: empty tunnel list');
  }

  const excludeSet = new Set(excludeUrls);
  const candidatePool = entry.tunnels.filter(t => !excludeSet.has(t.url));
  if (candidatePool.length === 0) {
    throw new Error('subs-resolver: all tunnels excluded');
  }
  const picked = pickWeighted(candidatePool);

  return {
    url: picked.url,
    allCandidates: entry.tunnels,
    source,
    fetchedAt: entry.fetchedAt,
  };
}

// Test-only exports.
export const __test__ = { parseSubsUrl, pickWeighted, fnv1a32hex };
