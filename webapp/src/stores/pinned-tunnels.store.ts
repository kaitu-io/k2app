/**
 * Pinned and recently used cloud tunnels (local only, per device).
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

export interface PinnedEntry {
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
  unpin: (domain: string) => void;
  isPinned: (domain: string) => boolean;
  recordRecent: (t: Tunnel) => void;
}

function toEntry(t: Tunnel): PinnedEntry {
  return {
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
        !!x && typeof x === 'object' && typeof (x as PinnedEntry).domain === 'string' && !!(x as PinnedEntry).domain)
      .map(x => ({
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
    if (pinned.some(p => p.domain === entry.domain) || pinned.length >= PIN_LIMIT) return;
    const nextPinned = [...pinned, entry];
    const nextRecent = recent.filter(r => r.domain !== entry.domain);
    console.info('[Pinned] pin ' + entry.domain + ' (' + nextPinned.length + ' pinned)');
    set({ pinned: nextPinned, recent: nextRecent });
    persist(PINNED_KEY, nextPinned);
    if (nextRecent.length !== recent.length) persist(RECENT_KEY, nextRecent);
  },

  unpin: (domain) => {
    const d = domain.toLowerCase();
    const next = get().pinned.filter(p => p.domain !== d);
    if (next.length === get().pinned.length) return;
    console.info('[Pinned] unpin ' + d + ' (' + next.length + ' pinned)');
    set({ pinned: next });
    persist(PINNED_KEY, next);
  },

  isPinned: (domain) => {
    const d = domain.toLowerCase();
    return get().pinned.some(p => p.domain === d);
  },

  recordRecent: (t) => {
    const entry = toEntry(t);
    if (get().isPinned(entry.domain)) return;
    const next = [entry, ...get().recent.filter(r => r.domain !== entry.domain)].slice(0, RECENT_LIMIT);
    set({ recent: next });
    persist(RECENT_KEY, next);
  },
}));
