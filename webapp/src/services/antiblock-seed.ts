// ---------------------------------------------------------------------------
// Antiblock cold-start seed module.
//
// New (or old) clients hitting a fresh network block need a relay-node pool to
// drive the direct→relay fallback. Today the pool (entry-pool.ts) is seeded
// ONLY by a successful /api/tunnels call — a chicken-and-egg on a brand-new (or
// long-idle) install during a block. This module closes the gap with:
//
//   1. A build-embedded floor seed (instant, zero network) — antiblock-seed-embedded.ts.
//   2. A CDN refresh that discovers the newest seed version by GALLOPING across
//      immutable `v/<N>.js` files.
//
// Why galloping: the target population is OLD installs hitting a NEW block.
// Their embedded floor cursor can be hundreds of versions behind current. A
// fixed-window probe (floor+1..floor+8) would only reach stale, likely-dead
// nodes. Galloping (floor+1, +2, +4, +8, … until a 404 frontier, then a binary
// narrow, then a small linear gap-confirm) lets even a months-old build jump to
// the CURRENT cursor in one launch. Files are immutable and never pruned, so the
// floor always exists on CDN and "any mirror has v/N = exists" converges to the
// global-max cursor (auto-absorbing per-mirror CDN staleness).
//
// All failures are swallowed — bootstrapAntiblockSeed() must NEVER throw.
// ---------------------------------------------------------------------------

import { loadJsonp, decrypt, type JsonpConfig } from './antiblock-crypto';
import { DECRYPTION_KEY, CDN_SOURCES } from './antiblock';
import { addNodes } from './entry-pool';
import type { NodeEntry } from './node-descriptor';
import { EMBEDDED_SEED } from './antiblock-seed-embedded';
import { RELAY_ENABLED } from './relay-flag';

// Distinct from the entry-config channel (global __k2ac, path ui.js/config.js).
// Never read that channel / __k2ac here.
export const SEED_GLOBAL = '__k2sd';

export const CURSOR_KEY = 'k2_seed_cursor';
// SAME key antiblock.resolveEntry reads — the seed can prime the entry URL.
export const ENTRY_KEY = 'k2_entry_url';
export const PROBE_AFTER_KEY = 'k2_seed_probe_after';
// First-launch marker (replaces the old pool-empty cold-start probe). Node
// storage moved to Go, so "is the pool empty?" is no longer a webapp question.
export const SEEDED_KEY = 'k2_relay_seeded';

export const PROBE_INTERVAL_MS = 6 * 60 * 60 * 1000;
export const GAP_CONFIRM = 4;

export interface SeedPayload {
  entries: string[];
  nodes: NodeEntry[];
}

// ---------------------------------------------------------------------------
// URL construction
// ---------------------------------------------------------------------------

export function seedPath(n: number): string {
  return 'v/' + n + '.js';
}

/** Map each CDN mirror (.../ui.js) to its versioned seed URL (.../v/<n>.js). */
export function seedUrls(n: number): string[] {
  return CDN_SOURCES.map(
    (src) => src.replace(/\/ui\.js$/, '/') + seedPath(n),
  );
}

// ---------------------------------------------------------------------------
// Decode
// ---------------------------------------------------------------------------

/** Decrypt + parse a seed JSONP config. null on v!==1 / garbage / decrypt-fail
 *  / shape-mismatch (entries or nodes not arrays). */
export async function decodeSeed(
  jsonp: JsonpConfig | null,
): Promise<SeedPayload | null> {
  if (!jsonp || jsonp.v !== 1 || typeof jsonp.data !== 'string') return null;
  const plaintext = await decrypt(jsonp.data, DECRYPTION_KEY);
  if (!plaintext) return null;
  try {
    const parsed = JSON.parse(plaintext) as Partial<SeedPayload>;
    if (!Array.isArray(parsed.entries) || !Array.isArray(parsed.nodes)) {
      return null;
    }
    return { entries: parsed.entries, nodes: parsed.nodes };
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// exists(n) — race all mirrors, first non-null decoded payload wins.
//
// Racing mirrors + "any hit = exists" converges to the GLOBAL max cursor across
// mirrors, auto-absorbing per-mirror s-maxage staleness. Inline race-to-first-
// truthy (no AbortController; loadJsonp resolves null on failure/timeout).
// ---------------------------------------------------------------------------

export async function exists(n: number): Promise<SeedPayload | null> {
  const urls = seedUrls(n);
  if (urls.length === 0) return null;
  return new Promise<SeedPayload | null>((resolve) => {
    let remaining = urls.length;
    let settled = false;
    for (const url of urls) {
      loadJsonp(url, SEED_GLOBAL)
        .then((cfg) => decodeSeed(cfg))
        .then((payload) => {
          if (settled) return;
          if (payload) {
            settled = true;
            resolve(payload);
            return;
          }
          if (--remaining === 0) resolve(null);
        })
        .catch(() => {
          if (settled) return;
          if (--remaining === 0) resolve(null);
        });
    }
  });
}

// ---------------------------------------------------------------------------
// findFrontier — galloping discovery of the highest existing cursor > floor.
// ---------------------------------------------------------------------------

export async function findFrontier(
  floor: number,
): Promise<{ cursor: number; payload: SeedPayload } | null> {
  // --- Gallop: floor+1, +2, +4, +8, … until first miss. ---
  let step = 1;
  let probe = floor + 1;
  let lastHit = -1;
  let lastHitPayload: SeedPayload | null = null;
  for (;;) {
    const payload = await exists(probe);
    if (!payload) break;
    lastHit = probe;
    lastHitPayload = payload;
    step *= 2;
    probe = floor + step;
  }
  if (lastHit === -1 || !lastHitPayload) return null;

  // --- Binary-narrow (lastHit exists, probe = firstMiss doesn't). ---
  let lo = lastHit;
  let hi = probe; // first miss
  let best = lastHit;
  let bestPayload = lastHitPayload;
  while (hi - lo > 1) {
    const mid = Math.floor((lo + hi) / 2);
    const payload = await exists(mid);
    if (payload) {
      lo = mid;
      best = mid;
      bestPayload = payload;
    } else {
      hi = mid;
    }
  }

  // --- Gap-confirm: bounded hole-bridging scan.
  //     Advances `best` past up to GAP_CONFIRM-1 consecutive missing versions
  //     (CI publish gaps), stopping only after GAP_CONFIRM consecutive misses.
  //     On any hit, best advances and the consecutive-miss counter resets,
  //     allowing the scan to keep climbing past sparse data. ---
  let misses = 0;
  let n = best + 1;
  while (misses < GAP_CONFIRM) {
    const p = await exists(n);
    if (p) { best = n; bestPayload = p; misses = 0; }
    else { misses++; }
    n++;
  }

  return { cursor: best, payload: bestPayload };
}

// ---------------------------------------------------------------------------
// localStorage guards (standalone / SSR safety)
// ---------------------------------------------------------------------------

function readNum(key: string): number {
  try {
    return Number(localStorage.getItem(key) || 0) || 0;
  } catch {
    return 0;
  }
}

function writeStr(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* best-effort */
  }
}

// ---------------------------------------------------------------------------
// bootstrapAntiblockSeed — never throws.
// ---------------------------------------------------------------------------

export async function bootstrapAntiblockSeed(): Promise<void> {
  if (!RELAY_ENABLED) {
    // 一次性修复：relay 时代的 seed bootstrap 每次启动都把直连缓存 k2_entry_url
    // 覆盖成 embedded entry（其 CloudFront 域对 CN 被 GFW 封）。值匹配才清除——
    // CDN 解析出的 entry 绝不误伤；清掉后 resolveEntry() 回到 CDN 解析 / 默认兜底。
    try {
      const cached = localStorage.getItem(ENTRY_KEY);
      if (cached !== null && EMBEDDED_SEED.entries.includes(cached)) {
        localStorage.removeItem(ENTRY_KEY);
      }
    } catch {
      /* best-effort */
    }
    return;
  }
  try {
    // (1) Always feed the embedded floor to the Go RelayManager. It is idempotent
    // (Go dedups by IP) and REQUIRED every launch: the manager's node store is
    // in-process, so it starts empty on each app/daemon start and must be re-primed
    // before the first relay-fetch. Cold start (first launch this install) is now
    // detected via a persisted marker, since the webapp no longer holds the pool.
    addNodes(EMBEDDED_SEED.nodes);
    if (EMBEDDED_SEED.entries[0]) writeStr(ENTRY_KEY, EMBEDDED_SEED.entries[0]);
    const coldStart = readNum(SEEDED_KEY) === 0;
    if (coldStart) {
      writeStr(SEEDED_KEY, '1');
      console.info(
        '[antiblock-seed] cold-start-used-embedded',
        'cursor=' + EMBEDDED_SEED.cursor,
        'nodes=' + EMBEDDED_SEED.nodes.length,
      );
    }

    // (2) Throttle gate: run network only on cold start or once the interval
    // has elapsed.
    const probeAfter = readNum(PROBE_AFTER_KEY);
    if (!coldStart && Date.now() < probeAfter) return;

    // (3) Floor = max(persisted cursor, embedded cursor).
    const floor = Math.max(readNum(CURSOR_KEY), EMBEDDED_SEED.cursor);

    // (4) Gallop the CDN for a newer cursor.
    try {
      const result = await findFrontier(floor);
      if (result && result.cursor > floor) {
        addNodes(result.payload.nodes);
        writeStr(CURSOR_KEY, String(result.cursor));
        if (result.payload.entries[0]) writeStr(ENTRY_KEY, result.payload.entries[0]);
        console.info(
          '[antiblock-seed] cursor-advanced',
          floor + '→' + result.cursor,
          'nodes=' + result.payload.nodes.length,
        );
      } else if (!result) {
        console.info('[antiblock-seed] probe-frontier none', 'floor=' + floor);
      }
    } catch (e) {
      console.warn('[antiblock-seed] probe-failed', e);
    }

    // (5) ALWAYS push the throttle forward after a network run.
    writeStr(PROBE_AFTER_KEY, String(Date.now() + PROBE_INTERVAL_MS));
  } catch (e) {
    // (6) Best-effort — never throw.
    console.warn('[antiblock-seed] probe-failed', e);
  }
}
