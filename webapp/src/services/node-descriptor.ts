import type { Tunnel } from './api-types';

/** A camouflage-node relay descriptor extracted from a tunnel's k2v5 serverUrl.
 *  No hop: the control-plane relay is TCP+TLS only (no QUIC port-hopping). */
export interface NodeEntry {
  ip: string;
  pin: string;
  ech: string;
}

/** Parse a k2v5:// (or k2wss://) serverUrl's query params. Returns {} on malformed input. */
export function parseServerUrl(serverUrl: string): { ip?: string; pin?: string; ech?: string } {
  try {
    // URL() needs an http(s) scheme to populate searchParams reliably.
    const u = new URL(serverUrl.replace(/^k2(v5|wss):\/\//, 'https://'));
    return {
      ip: u.searchParams.get('ip') ?? undefined,
      pin: u.searchParams.get('pin') ?? undefined,
      ech: u.searchParams.get('ech') ?? undefined,
    };
  } catch {
    return {};
  }
}

/** Build relay NodeEntry[] from a tunnels response. Tunnels lacking pin/ech/ip are skipped.
 *  ip precedence: serverUrl ?ip= → tunnel.node.ipv4. Deduped by ip (first wins). */
export function nodeEntriesFromTunnels(items: Tunnel[]): NodeEntry[] {
  const out: NodeEntry[] = [];
  const seen = new Set<string>();
  for (const t of items) {
    if (!t.serverUrl) continue;
    const p = parseServerUrl(t.serverUrl);
    const ip = p.ip || t.node?.ipv4;
    if (!ip || !p.pin || !p.ech) continue;
    if (seen.has(ip)) continue;
    seen.add(ip);
    out.push({ ip, pin: p.pin, ech: p.ech });
  }
  return out;
}
