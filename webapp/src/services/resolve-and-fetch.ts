import { resolveEntry } from './antiblock';
import * as pool from './entry-pool';
import type { NodeEntry } from './node-descriptor';
import type { RelayRequest, RelayResponse, SResponse } from '../types/kaitu-core';

/** Inner-SNI control-plane routing label — MUST match node-side control_plane_routes (Phase 1). */
export const CONTROL_PLANE_HOST = 'k2.52j.me';

const DIRECT_PROBE_TIMEOUT_MS = 5000;
// MUST stay < cloud-api REQUEST_TIMEOUT_MS (15s) minus DIRECT_PROBE_TIMEOUT_MS,
// so that when relay-first abandons a hung node the direct FALLBACK still fits
// inside the total request budget (9 + 5 = 14 < 15). If relay could consume the
// whole 15s, the outer withTimeout would fire before direct ever runs, silently
// defeating the fallback. (Dead nodes fail fast via 502; this guards the
// black-hole/hang case — exactly the GFW scenario.)
const RELAY_TIMEOUT_MS = 9000;
const RELAY_FANOUT = 6;

export type TransportResult =
  | { transport: 'ok'; status: number; json: () => Promise<any> }
  | { transport: 'fail' };

interface RelayReq {
  method: string;
  path: string;
  headers: Record<string, string>;
  body?: string;
}

// First-resolved race (Promise.any is ES2021; build target is ES2020).
function firstResolved<T>(promises: Promise<T>[]): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let remaining = promises.length;
    if (remaining === 0) { reject(new Error('empty')); return; }
    for (const p of promises) {
      p.then(resolve, () => { if (--remaining === 0) reject(new Error('all failed')); });
    }
  });
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
    pool.clearDirectBlocked();
    return { transport: 'ok', status: resp.status, json: () => resp.json() };
  } catch {
    clearTimeout(timer);
    pool.markDirectBlocked();
    return null; // connection-level failure → caller falls back to relay
  }
}

async function relayOne(node: NodeEntry, req: RelayReq): Promise<{ status: number; body: string }> {
  const k2 = (window as unknown as { _k2?: { run: (a: string, p: RelayRequest) => Promise<SResponse<RelayResponse>> } })._k2;
  if (!k2) throw new Error('no _k2');
  const relayReq: RelayRequest = {
    ip: node.ip,
    pin: node.pin,
    ech: node.ech,
    centerHost: CONTROL_PLANE_HOST,
    method: req.method,
    path: req.path,
    headers: req.headers,
    body: req.body,
  };
  // Race the IPC call against a per-relay timeout so a stalled node/daemon
  // never keeps firstResolved pending indefinitely (§5.3 review #5 c).
  const ipcPromise = k2.run('relay-fetch', relayReq);
  let timer: ReturnType<typeof setTimeout>;
  const timeoutPromise = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      pool.recordFailure(node.ip);
      reject(new Error('relay timeout'));
    }, RELAY_TIMEOUT_MS);
  });
  let resp: SResponse<RelayResponse>;
  try {
    resp = await Promise.race([ipcPromise, timeoutPromise]);
  } finally {
    clearTimeout(timer!);
  }
  if (resp.code === 0 && resp.data) {
    pool.recordSuccess(node.ip);
    return { status: resp.data.status, body: resp.data.body };
  }
  if (resp.code === -1) {
    // -1 = relay unsupported on this build/platform (capacitor / daemon-less
    // standalone / daemon unreachable) — a CAPABILITY signal, not a node fault.
    // Don't penalise the node's score; learn it so we stop trying relay this
    // session and use direct. Node faults return 502, handled below.
    pool.markRelayUnsupported();
    throw new Error('relay unsupported');
  }
  pool.recordFailure(node.ip);
  throw new Error('relay failed code=' + resp.code);
}

async function tryRelay(req: RelayReq): Promise<TransportResult> {
  const nodes = pool.getNodes().slice(0, RELAY_FANOUT);
  if (nodes.length === 0) return { transport: 'fail' };
  try {
    const { status, body } = await firstResolved(nodes.map(n => relayOne(n, req)));
    return { transport: 'ok', status, json: async () => JSON.parse(body) };
  } catch {
    return { transport: 'fail' };
  }
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
  if (pool.isRelaySupported()) {
    const relay = await tryRelay(req);
    if (relay.transport === 'ok') return relay;
  }
  const direct = await tryDirect(req);
  if (direct) return direct;
  return { transport: 'fail' };
}
