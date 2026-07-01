import type { NodeEntry } from './node-descriptor';

const POOL_KEY = 'k2_node_pool';
const STICKY_KEY = 'k2_direct_blocked_until';

const MAX_NODES = 64;
const MAX_SCORE = 8;
const MIN_SCORE = -4;
const PRUNE_MS = 7 * 24 * 60 * 60 * 1000;
const STICKY_TTL_MS = 5 * 60 * 1000;

interface ScoredNode extends NodeEntry {
  score: number;
  lastOkAt: number;
  lastFailAt: number;
}

function load(): ScoredNode[] {
  try {
    const raw = localStorage.getItem(POOL_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((n): n is ScoredNode =>
      n && typeof n.ip === 'string' && typeof n.pin === 'string' && typeof n.ech === 'string');
  } catch {
    return [];
  }
}

function save(nodes: ScoredNode[]): void {
  // prune stale (no success in 7d, and that ever had a chance — lastOkAt===0 means never succeeded yet, keep)
  const now = Date.now();
  let kept = nodes.filter(n => n.lastOkAt === 0 || now - n.lastOkAt < PRUNE_MS);
  // cap: drop worst (lowest score, then oldest lastOkAt)
  if (kept.length > MAX_NODES) {
    kept = kept
      .slice()
      .sort((a, b) => b.score - a.score || b.lastOkAt - a.lastOkAt)
      .slice(0, MAX_NODES);
  }
  try {
    localStorage.setItem(POOL_KEY, JSON.stringify(kept));
  } catch {
    /* quota / unavailable — best-effort */
  }
}

export function addNodes(entries: NodeEntry[]): void {
  const nodes = load();
  const byIp = new Map(nodes.map(n => [n.ip, n]));
  for (const e of entries) {
    if (!e.ip || !e.pin || !e.ech) continue;
    const existing = byIp.get(e.ip);
    if (existing) {
      // refresh descriptor (ech/pin may have rotated), keep score history
      existing.pin = e.pin;
      existing.ech = e.ech;
    } else {
      byIp.set(e.ip, { ip: e.ip, pin: e.pin, ech: e.ech, score: 1, lastOkAt: 0, lastFailAt: 0 });
    }
  }
  save([...byIp.values()]);
}

export function getNodes(): NodeEntry[] {
  const now = Date.now();
  const nodes = load().filter(n => n.lastOkAt === 0 || now - n.lastOkAt < PRUNE_MS);
  nodes.sort((a, b) => b.score - a.score || b.lastOkAt - a.lastOkAt);
  return nodes.map(n => ({ ip: n.ip, pin: n.pin, ech: n.ech }));
}

function adjust(ip: string, delta: number, ok: boolean): void {
  const nodes = load();
  const n = nodes.find(x => x.ip === ip);
  if (!n) return;
  n.score = Math.max(MIN_SCORE, Math.min(MAX_SCORE, n.score + delta));
  if (ok) n.lastOkAt = Date.now();
  else n.lastFailAt = Date.now();
  save(nodes);
}

export function recordSuccess(ip: string): void { adjust(ip, +1, true); }
export function recordFailure(ip: string): void { adjust(ip, -1, false); }

export function isDirectBlocked(): boolean {
  try {
    const until = Number(localStorage.getItem(STICKY_KEY) || 0);
    return Date.now() < until;
  } catch {
    return false;
  }
}

export function markDirectBlocked(): void {
  try {
    localStorage.setItem(STICKY_KEY, String(Date.now() + STICKY_TTL_MS));
  } catch {
    /* best-effort */
  }
}

export function clearDirectBlocked(): void {
  try {
    localStorage.removeItem(STICKY_KEY);
  } catch {
    /* best-effort */
  }
}

// --- Relay capability (session-scoped, in-memory) ---------------------------
// Relay-first is the transport order, but relay is unsupported on web/mobile
// (capacitor returns code:-1; a daemon-less standalone fetch also yields -1).
// The first such -1 flips this flag so subsequent requests skip the doomed
// relay attempt and go straight to direct. In-memory (not persisted) so a
// transient daemon-down on desktop self-heals on the next app launch — never a
// permanent "relay off".
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
