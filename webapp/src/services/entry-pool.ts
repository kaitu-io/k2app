import type { NodeEntry } from './node-descriptor';
import type { SResponse } from '../types/kaitu-core';
import { EMBEDDED_SEED } from './antiblock-seed-embedded';

// The webapp is NO LONGER the authority for relay nodes. Node storage, ranking,
// health, single-active-host selection, and connection reuse all live in the Go
// RelayManager (k2/wire/relay_manager.go). This module is now only:
//   1. a FEEDER — forwards camouflage-node descriptors it discovers (embedded
//      seed + CDN refresh + /api/tunnels) to Go via relay-add-nodes; and
//   2. the session-scoped relay-capability flag the transport layer
//      (resolve-and-fetch.ts) still needs on the webapp side.
// It holds NO node list and does NO scoring — that would be a second, divergent
// source of truth. (See the "authority model" design: origins → Go single store.)

/**
 * Forward discovered camouflage-node descriptors to the Go RelayManager
 * (relay-add-nodes — incremental, deduped by IP in Go). Fire-and-forget: Go owns
 * the merge + persistence + ranking, so the webapp neither awaits nor stores the
 * result. A no-op when relay is unsupported (web / daemon-less) or _k2 absent.
 */
export function addNodes(entries: NodeEntry[]): void {
  if (!relaySupported || entries.length === 0) return;
  const k2 = (window as unknown as { _k2?: { run: (a: string, p: unknown) => Promise<SResponse<unknown>> } })._k2;
  if (!k2) return;
  try {
    void Promise.resolve(k2.run('relay-add-nodes', { nodes: entries })).catch(() => { /* best-effort */ });
  } catch {
    /* _k2.run threw synchronously (stub) — best-effort */
  }
}

// SEED_PRIME_TIMEOUT_MS bounds the awaited embedded-seed prime so a hung/not-yet-
// ready native bridge can never block app startup — if it fires we proceed unprimed
// (degrades to the old fire-and-forget behaviour for that one cold start).
const SEED_PRIME_TIMEOUT_MS = 2000;

let seedPrimed: Promise<void> | null = null;

/**
 * Feed the embedded camouflage-node floor into the Go RelayManager and resolve
 * ONLY after Go has ingested it (unlike fire-and-forget addNodes). The bootstrap
 * awaits this BEFORE the first cloud request so a cold start — a fresh install
 * hitting a live block — never issues a relay-fetch against an empty Go pool
 * (which returns 502 → falls back to direct on the very network that is blocked).
 * Memoized once it succeeds; a failed prime clears the memo so a later caller
 * retries (and the ongoing addNodes discovery feeds cover the gap meanwhile).
 * Internally time-bounded so a hung bridge never blocks the caller.
 */
export function ensureSeeded(): Promise<void> {
  if (seedPrimed) return seedPrimed;
  const p = (async () => {
    if (!relaySupported || EMBEDDED_SEED.nodes.length === 0) return;
    const k2 = (window as unknown as { _k2?: { run: (a: string, p: unknown) => Promise<SResponse<unknown>> } })._k2;
    if (!k2) return;
    let timer: ReturnType<typeof setTimeout>;
    const bound = new Promise<void>((r) => { timer = setTimeout(r, SEED_PRIME_TIMEOUT_MS); });
    try {
      await Promise.race([
        Promise.resolve(k2.run('relay-add-nodes', { nodes: EMBEDDED_SEED.nodes })).then(() => undefined),
        bound,
      ]);
    } finally {
      clearTimeout(timer!);
    }
  })().catch(() => {
    // Prime attempt failed → clear the memo so the next caller retries.
    seedPrimed = null;
  });
  seedPrimed = p;
  return p;
}

// Test-only: reset the memoized embedded-seed prime between cases.
export function __resetSeededForTest(): void {
  seedPrimed = null;
}

// --- Relay capability (session-scoped, in-memory) ---------------------------
// Relay-first is the transport order. A code:-1 from _k2.run('relay-fetch') is a
// CAPABILITY signal — the platform genuinely cannot relay: web (no core), a
// daemon-less / daemon-down desktop, or an old build with no native relay method.
// The first such -1 flips this flag so subsequent requests skip the doomed relay
// and go straight to direct. In-memory (not persisted) so a transient daemon-down
// self-heals on the next launch — never a permanent "relay off".
// NOTE: wired mobile (iOS/Android) DOES support relay. Its not-ready-yet states
// (e.g. Android's service-bind window) return a TRANSIENT code (503), NOT -1, so
// a startup race can never disable relay for the whole session and strand a
// blocked client on direct. Go's own no-nodes / all-exhausted case is 502.
let relaySupported = true;

export function isRelaySupported(): boolean {
  return relaySupported;
}

export function markRelayUnsupported(): void {
  relaySupported = false;
}

// Test-only: reset the session-scoped relay-capability flag between cases.
export function __resetRelaySupportForTest(): void {
  relaySupported = true;
}
