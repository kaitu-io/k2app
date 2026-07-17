import { resolveEntry } from './antiblock';
import * as pool from './entry-pool';
import type { RelayRequest, RelayResponse, SResponse } from '../types/kaitu-core';
import { RELAY_ENABLED } from './relay-flag';

/** Inner-SNI control-plane routing label — MUST match node-side control_plane_routes (Phase 1). */
export const CONTROL_PLANE_HOST = 'k2.52j.me';

// Relay disabled → direct is the ONLY transport and owns (almost) the whole
// 15s cloud-api budget (14s, leaving 1s headroom under the outer withTimeout).
// When relay is primary, direct is the 5s fallback probe — see budget note below.
// (14s headroom math assumes a cached entry; on a cold start resolveEntry's CDN
// race runs first and the outer 15s withTimeout is the effective ceiling.)
const DIRECT_PROBE_TIMEOUT_MS = RELAY_ENABLED ? 5000 : 14000;
// MUST stay < cloud-api REQUEST_TIMEOUT_MS (15s) minus DIRECT_PROBE_TIMEOUT_MS,
// so that when relay-first abandons a hung sweep the direct FALLBACK still fits
// inside the total request budget (9 + 5 = 14 < 15). If relay could consume the
// whole 15s, the outer withTimeout would fire before direct ever runs, silently
// defeating the fallback. Go's RelayManager sequences node failover internally
// under its own 9s budget; this webapp cap guards the pathological hung-IPC case.
const RELAY_TIMEOUT_MS = 9000;

export type TransportResult =
  | { transport: 'ok'; status: number; json: () => Promise<any> }
  | { transport: 'fail' };

interface RelayReq {
  method: string;
  path: string;
  headers: Record<string, string>;
  body?: string;
}

async function tryDirect(req: RelayReq): Promise<TransportResult | null> {
  const entry = await resolveEntry();
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), DIRECT_PROBE_TIMEOUT_MS);
  try {
    const resp = await fetch(entry + req.path, {
      method: req.method,
      headers: req.headers,
      body: req.body,
      signal: controller.signal,
    });
    clearTimeout(timer);
    return { transport: 'ok', status: resp.status, json: () => resp.json() };
  } catch {
    clearTimeout(timer);
    return null; // connection-level failure → caller falls back to relay
  }
}

// tryRelay sends ONE node-less relay request. Node selection, ranking, sequential
// failover across nodes, and keep-alive connection reuse all happen inside the Go
// RelayManager — the webapp neither picks a node nor knows which one served the
// request. Envelope codes: 0 = success (any HTTP status, incl. 401, passed
// through verbatim); -1 = relay UNSUPPORTED (capability downgrade → learn +
// direct); 502 = relay failed (no usable nodes / all exhausted → direct); 503 =
// transient not-ready (→ direct this time, relay stays enabled). The 9s cap
// guards a pathologically hung IPC so the direct fallback still fits the budget.
async function tryRelay(req: RelayReq): Promise<TransportResult> {
  const k2 = (window as unknown as { _k2?: { run: (a: string, p: RelayRequest) => Promise<SResponse<RelayResponse>> } })._k2;
  if (!k2) return { transport: 'fail' };
  const relayReq: RelayRequest = {
    centerHost: CONTROL_PLANE_HOST,
    method: req.method,
    path: req.path,
    headers: req.headers,
    body: req.body,
  };
  let timer: ReturnType<typeof setTimeout>;
  const timeoutPromise = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('relay timeout')), RELAY_TIMEOUT_MS);
  });
  let resp: SResponse<RelayResponse>;
  try {
    resp = await Promise.race([k2.run('relay-fetch', relayReq), timeoutPromise]);
  } catch {
    return { transport: 'fail' };
  } finally {
    clearTimeout(timer!);
  }
  if (resp.code === 0 && resp.data) {
    const data = resp.data;
    return { transport: 'ok', status: data.status, json: async () => JSON.parse(data.body) };
  }
  if (resp.code === -1) {
    // Capability signal (web / daemon-less / old build) — learn it so subsequent
    // requests skip the doomed relay. Node faults are 502, transient is 503.
    pool.markRelayUnsupported();
  }
  return { transport: 'fail' };
}

/**
 * Resolve the best transport and perform one request: camouflage-node relay
 * FIRST (when relay is supported), then direct fetch as the fallback. The relay
 * path is identical for blocked and unblocked clients, so it is the primary
 * transport — and exercising it from anywhere represents in-region behaviour.
 * Direct is the safety net (permanent control-plane host, fleet-independent) and
 * the only path on web/mobile, where relay is unsupported (learned via code:-1).
 * NEVER handles 401 — returns the status verbatim so cloud-api's _handle401 owns
 * refresh.
 */
export async function resolveAndFetch(req: RelayReq): Promise<TransportResult> {
  if (RELAY_ENABLED && pool.isRelaySupported()) {
    const relay = await tryRelay(req);
    if (relay.transport === 'ok') return relay;
  }
  const direct = await tryDirect(req);
  if (direct) return direct;
  return { transport: 'fail' };
}
