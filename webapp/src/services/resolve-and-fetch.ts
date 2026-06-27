import { resolveEntry } from './antiblock';
import * as pool from './entry-pool';
import type { NodeEntry } from './node-descriptor';
import type { RelayRequest, RelayResponse, SResponse } from '../types/kaitu-core';

/** Inner-SNI control-plane routing label — MUST match node-side control_plane_routes (Phase 1). */
export const CONTROL_PLANE_HOST = 'k2.52j.me';

const DIRECT_PROBE_TIMEOUT_MS = 5000;
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
  const resp = await k2.run('relay-fetch', relayReq);
  if (resp.code === 0 && resp.data) {
    pool.recordSuccess(node.ip);
    return { status: resp.data.status, body: resp.data.body };
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
 * Resolve the best transport and perform one request: direct fetch first
 * (unless sticky-blocked), then camouflage-node relay fallback. NEVER handles
 * 401 — returns the status verbatim so cloud-api's _handle401 owns refresh.
 */
export async function resolveAndFetch(req: RelayReq): Promise<TransportResult> {
  if (!pool.isDirectBlocked()) {
    const direct = await tryDirect(req);
    if (direct) return direct;
  }
  return tryRelay(req);
}
