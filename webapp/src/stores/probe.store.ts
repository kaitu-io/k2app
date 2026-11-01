import { create } from 'zustand';
import type { ProbeResult } from '../services/api-types';

const STALE_MS = 15 * 60 * 1000; // mirrors k2/probe Registry default TTL

/** Extract hostname from a k2v5 URL for store keying. */
function domainOf(rawUrl: string): string {
  try {
    // Swap scheme: URL() tolerates custom schemes but some parsers prefer https.
    return new URL(rawUrl.replace(/^k2v\d+:\/\//, 'https://')).hostname.toLowerCase();
  } catch {
    return rawUrl;
  }
}

interface ProbeState {
  results: Map<string, ProbeResult>;
  inFlight: Set<string>;
  lastUpdated: number;

  record: (incoming: ProbeResult[]) => void;
  markInFlight: (domains: string[]) => void;
  clearInFlight: (domains: string[]) => void;
  /**
   * Returns the probeScore for a domain, or null if:
   *   - no record exists
   *   - record is older than STALE_MS
   *   - record is the -1 sentinel (reachable but unmeasured)
   */
  getScore: (domain: string) => number | null;
  getResult: (domain: string) => ProbeResult | null;
}

export const useProbeStore = create<ProbeState>((set, get) => ({
  results: new Map(),
  inFlight: new Set(),
  lastUpdated: 0,

  record: (incoming) => {
    set((state) => {
      const next = new Map(state.results);
      for (const r of incoming) next.set(domainOf(r.url), r);
      return { results: next, lastUpdated: Date.now() };
    });
  },

  markInFlight: (domains) => {
    set((state) => {
      const next = new Set(state.inFlight);
      for (const d of domains) next.add(d.toLowerCase());
      return { inFlight: next };
    });
  },

  clearInFlight: (domains) => {
    set((state) => {
      const next = new Set(state.inFlight);
      for (const d of domains) next.delete(d.toLowerCase());
      return { inFlight: next };
    });
  },

  getScore: (domain) => {
    const r = get().results.get(domain.toLowerCase());
    if (!r) return null;
    const age = Date.now() - Date.parse(r.measuredAt);
    if (age > STALE_MS) return null;
    if (r.probeScore < 0) return null;
    return r.probeScore;
  },

  getResult: (domain) => get().results.get(domain.toLowerCase()) ?? null,
}));
