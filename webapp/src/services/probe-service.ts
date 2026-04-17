import type { ProbeResponse, Tunnel } from './api-types';
import { useProbeStore } from '../stores/probe.store';
import { useVPNMachineStore } from '../stores/vpn-machine.store';

const PROBE_TIMEOUT_MS = 8000;

function domainOf(rawUrl: string): string {
  try {
    return new URL(rawUrl.replace(/^k2v\d+:\/\//, 'https://')).hostname.toLowerCase();
  } catch {
    return rawUrl;
  }
}

/**
 * Trigger a one-shot daemon probe for the given tunnels and record results
 * into probe.store. Silently skipped when:
 *   - tunnels is empty
 *   - platform is 'web' (no real QUIC available)
 *   - VPN is not idle (prevents probe double-encapsulation on mobile NE)
 */
export async function runProbe(tunnels: Tunnel[]): Promise<void> {
  if (tunnels.length === 0) return;
  if (window._platform?.platformType === 'web') return;

  const vpnState = useVPNMachineStore.getState().state;
  if (vpnState !== 'idle' && vpnState !== 'serviceDown') return;

  const urls = tunnels.map((t) => t.serverUrl).filter(Boolean) as string[];
  if (urls.length === 0) return;
  const domains = tunnels
    .map((t) => (t.serverUrl ? domainOf(t.serverUrl) : ''))
    .filter(Boolean);

  useProbeStore.getState().markInFlight(domains);
  try {
    const resp = await window._k2.run<ProbeResponse>('probe', {
      urls,
      timeoutMs: PROBE_TIMEOUT_MS,
    });
    if (resp.code === 0 && resp.data?.results) {
      useProbeStore.getState().record(resp.data.results);
    } else {
      console.warn('[probe-service] daemon probe code=%d msg=%s', resp.code, resp.message);
    }
  } catch (err) {
    console.warn('[probe-service] probe error', err);
  } finally {
    useProbeStore.getState().clearInFlight(domains);
  }
}
