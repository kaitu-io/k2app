/**
 * Pinned and recently used cloud nodes (local only, per device).
 *
 * Identity is the node IP (`nodeKey`: node ipv4, else ipv6, else the tunnel
 * domain as a last resort) — the backend's unique node key, and what users
 * actually care about. A pin follows the IP, not the tunnel domain: when ops
 * changes a node's IP the domain survives, so a domain-keyed pin would
 * silently move the user to a new IP. Keyed by IP, it shows as unavailable.
 *
 * Pins are the user's own failover pool: connecting to a pinned tunnel sends
 * the other pins as `ClientConfig.fallbacks` (connection.store), so the engine
 * never switches outside the pool. Recent = last few connected tunnels that
 * are not pinned, offered for one-tap pinning.
 *
 * Spec: docs/superpowers/specs/2026-11-10-pinned-tunnels-design.md
 */
import { create } from 'zustand';
import type { Tunnel } from '../services/api-types';
import { nodeKey } from '../utils/auto-tunnel-pick';

export interface PinnedEntry {
  /** Node identity — see nodeKey(). */
  key: string;
  /** Tunnel last used on this node; preferred when the node has several. */
  domain: string;
  name: string;
  country: string;
  /** pinnedAt for pins, lastUsedAt for recent (ms). */
  at: number;
}

export const PIN_LIMIT = 10;
export const RECENT_LIMIT = 3;

const PINNED_KEY = 'k2.connection.pinnedTunnels';
const RECENT_KEY = 'k2.connection.recentTunnels';

interface PinnedTunnelsState {
  pinned: PinnedEntry[];
  recent: PinnedEntry[];
  loaded: boolean;
  load: () => Promise<void>;
  pin: (t: Tunnel) => void;
  unpin: (key: string) => void;
  isPinned: (t: Tunnel) => boolean;
  recordRecent: (t: Tunnel) => void;
}

function toEntry(t: Tunnel): PinnedEntry {
  return {
    key: nodeKey(t),
    domain: t.domain.toLowerCase(),
    name: t.name || t.domain,
    country: t.node?.country ?? '',
    at: Date.now(),
  };
}

function parseEntries(raw: unknown): PinnedEntry[] {
  if (typeof raw !== 'string' || !raw) return [];
  try {
    const arr: unknown = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr
      .filter((x): x is PinnedEntry =>
        !!x && typeof x === 'object'
        && typeof (x as PinnedEntry).key === 'string' && !!(x as PinnedEntry).key
        && typeof (x as PinnedEntry).domain === 'string')
      .map(x => ({
        key: x.key,
        domain: x.domain.toLowerCase(),
        name: typeof x.name === 'string' ? x.name : x.domain,
        country: typeof x.country === 'string' ? x.country : '',
        at: typeof x.at === 'number' ? x.at : 0,
      }));
  } catch {
    return [];
  }
}

function persist(key: string, entries: PinnedEntry[]): void {
  const write = entries.length === 0
    ? window._platform.storage.remove(key)
    : window._platform.storage.set(key, JSON.stringify(entries));
  Promise.resolve(write).catch(err => console.warn('[Pinned] Failed to persist ' + key + ':', err));
}

export const usePinnedTunnelsStore = create<PinnedTunnelsState>()((set, get) => ({
  pinned: [],
  recent: [],
  loaded: false,

  load: async () => {
    try {
      const [p, r] = await Promise.all([
        window._platform.storage.get<string>(PINNED_KEY),
        window._platform.storage.get<string>(RECENT_KEY),
      ]);
      set({ pinned: parseEntries(p).slice(0, PIN_LIMIT), recent: parseEntries(r).slice(0, RECENT_LIMIT), loaded: true });
    } catch (err) {
      console.warn('[Pinned] Failed to load:', err);
      set({ loaded: true });
    }
  },

  pin: (t) => {
    const entry = toEntry(t);
    const { pinned, recent } = get();
    if (pinned.some(p => p.key === entry.key) || pinned.length >= PIN_LIMIT) return;
    const nextPinned = [...pinned, entry];
    const nextRecent = recent.filter(r => r.key !== entry.key);
    console.info('[Pinned] pin ' + entry.key + ' via ' + entry.domain + ' (' + nextPinned.length + ' pinned)');
    set({ pinned: nextPinned, recent: nextRecent });
    persist(PINNED_KEY, nextPinned);
    if (nextRecent.length !== recent.length) persist(RECENT_KEY, nextRecent);
  },

  unpin: (key) => {
    const next = get().pinned.filter(p => p.key !== key);
    if (next.length === get().pinned.length) return;
    console.info('[Pinned] unpin ' + key + ' (' + next.length + ' pinned)');
    set({ pinned: next });
    persist(PINNED_KEY, next);
  },

  isPinned: (t) => {
    const k = nodeKey(t);
    return get().pinned.some(p => p.key === k);
  },

  recordRecent: (t) => {
    const entry = toEntry(t);
    if (get().isPinned(t)) return;
    const next = [entry, ...get().recent.filter(r => r.key !== entry.key)].slice(0, RECENT_LIMIT);
    set({ recent: next });
    persist(RECENT_KEY, next);
  },
}));

/**
 * The tunnel to use for a pinned/recent node: one on the same node (IP) with a
 * `serverUrl`, preferring the tunnel last used there. `undefined` = the node is
 * not in the list (offline, removed, or its IP changed).
 */
export function resolveEntry(entry: Pick<PinnedEntry, 'key' | 'domain'>, tunnels: readonly Tunnel[]): Tunnel | undefined {
  let any: Tunnel | undefined;
  for (const t of tunnels) {
    if (!t.serverUrl || nodeKey(t) !== entry.key) continue;
    if (t.domain.toLowerCase() === entry.domain) return t;
    any ??= t;
  }
  return any;
}
