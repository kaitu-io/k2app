/**
 * Connection Store — connection target selection + connect/disconnect orchestration
 *
 * Owns:
 * - Which tunnel source is selected (cloud vs self-hosted)
 * - Which specific cloud tunnel is selected
 * - Derived activeTunnel (computed from selection)
 * - connectedTunnel snapshot (stable during active connection)
 * - connectEpoch guard (prevents stale async operations)
 * - connect() / disconnect() orchestration
 *
 * Does NOT own:
 * - VPN state machine (vpn-machine.store.ts)
 * - Config persistence details (config.store.ts)
 * - Self-hosted tunnel management (self-hosted.store.ts)
 */

import { create } from 'zustand';
import type { Tunnel } from '../services/api-types';
import { authService } from '../services/auth-service';
import { useSelfHostedStore } from './self-hosted.store';
import { useConfigStore } from './config.store';
import { useVPNMachineStore, dispatch as vpnDispatch } from './vpn-machine.store';

// ============ Types ============

export interface ActiveTunnel {
  source: 'cloud' | 'self_hosted';
  domain: string;
  name: string;
  country: string;
  serverUrl: string;
}

interface ConnectionState {
  selectedSource: 'cloud' | 'self_hosted';
  selectedCloudTunnel: Tunnel | null;
  activeTunnel: ActiveTunnel | null;
  connectedTunnel: ActiveTunnel | null;
  connectEpoch: number;
}

interface ConnectionActions {
  selectCloudTunnel: (tunnel: Tunnel) => void;
  selectSelfHosted: () => void;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
}

// ============ Helpers ============

function computeCloudActiveTunnel(tunnel: Tunnel): ActiveTunnel {
  return {
    source: 'cloud',
    domain: tunnel.domain.toLowerCase(),
    name: tunnel.name || tunnel.domain,
    country: tunnel.node?.country || '',
    serverUrl: tunnel.serverUrl || '',
  };
}

function computeSelfHostedActiveTunnel(): ActiveTunnel | null {
  const selfHosted = useSelfHostedStore.getState().tunnel;
  if (!selfHosted) return null;

  let domain = '';
  try {
    const parsed = new URL(selfHosted.uri.replace('k2v5://', 'https://'));
    domain = parsed.hostname;
  } catch { /* ignore */ }

  return {
    source: 'self_hosted',
    domain,
    name: selfHosted.name,
    country: selfHosted.country || '',
    serverUrl: selfHosted.uri,
  };
}

// ============ Store ============

export const useConnectionStore = create<ConnectionState & ConnectionActions>()((set, get) => ({
  // State
  selectedSource: 'cloud',
  selectedCloudTunnel: null,
  activeTunnel: null,
  connectedTunnel: null,
  connectEpoch: 0,

  // Actions
  selectCloudTunnel: (tunnel) => {
    console.info('[Connection] selectCloudTunnel: domain=' + tunnel.domain + ', name=' + (tunnel.name || tunnel.domain));
    set({
      selectedSource: 'cloud',
      selectedCloudTunnel: tunnel,
      activeTunnel: computeCloudActiveTunnel(tunnel),
    });
  },

  selectSelfHosted: () => {
    const tunnel = computeSelfHostedActiveTunnel();
    console.info('[Connection] selectSelfHosted: domain=' + (tunnel?.domain ?? 'null'));
    set({
      selectedSource: 'self_hosted',
      activeTunnel: tunnel,
    });
  },

  connect: async () => {
    const t0 = Date.now();
    // State guard: reject if already connecting/connected/reconnecting/disconnecting.
    // Prevents double-click sending duplicate _k2.run('up') — daemon's opMu serializes
    // but would cause unnecessary disconnect+reconnect cycle.
    const vpnState = useVPNMachineStore.getState().state;
    console.warn('[Connection] TRACE connect START t=' + t0 + ' vpnState=' + vpnState);
    if (vpnState !== 'idle' && vpnState !== 'error' && vpnState !== 'serviceDown') {
      console.warn('[Connection] connect: rejected (vpnState=' + vpnState + ')');
      return;
    }

    const { selectedSource, selectedCloudTunnel, activeTunnel, connectEpoch } = get();
    if (!activeTunnel) {
      console.warn('[Connection] connect: no activeTunnel, aborting');
      return;
    }

    const myEpoch = connectEpoch + 1;
    console.info('[Connection] connect: source=' + selectedSource + ', tunnel=' + activeTunnel.domain + ', epoch=' + connectEpoch + '→' + myEpoch);
    set({ connectedTunnel: activeTunnel, connectEpoch: myEpoch });
    console.warn('[Connection] TRACE connectedTunnel set t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Resolve server URL
    let serverUrl: string | undefined;
    if (selectedSource === 'self_hosted') {
      serverUrl = useSelfHostedStore.getState().tunnel?.uri;
    } else if (selectedCloudTunnel?.serverUrl) {
      serverUrl = await authService.buildTunnelUrl(selectedCloudTunnel.serverUrl);
    }
    console.warn('[Connection] TRACE buildTunnelUrl done t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Epoch guard: bail if user disconnected or started new connect
    if (get().connectEpoch !== myEpoch) {
      console.warn('[Connection] connect: epoch mismatch (mine=' + myEpoch + ', current=' + get().connectEpoch + '), aborting');
      return;
    }

    // Build config with explicit params
    const { buildConnectConfig, updateConfig } = useConfigStore.getState();
    const isBeta = window._platform?.updater?.channel === 'beta';
    const logLevel = localStorage.getItem('k2_log_level') || 'info';
    const config = buildConnectConfig({ serverUrl, isBeta, logLevel });
    console.debug('[Connection] connect: config built, server=' + (config.server ?? 'none') + ', rule=' + (config.rule?.global ? 'global' : 'chnroute') + ', logLevel=' + config.log?.level);

    // Persist BEFORE _k2.run so crash doesn't lose config
    await updateConfig({ server: serverUrl });
    console.warn('[Connection] TRACE updateConfig done t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Dispatch state machine event and execute
    console.warn('[Connection] TRACE USER_CONNECT dispatch t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');
    vpnDispatch('USER_CONNECT');
    try {
      await window._k2.run('up', config);
      console.warn('[Connection] TRACE _k2.run(up) returned t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');
    } catch (err) {
      console.error('[Connection] connect failed:', err);
    }
  },

  disconnect: async () => {
    // State guard: reject if already disconnecting or idle.
    const vpnState = useVPNMachineStore.getState().state;
    if (vpnState === 'disconnecting' || vpnState === 'idle') {
      console.warn('[Connection] disconnect: rejected (vpnState=' + vpnState + ')');
      return;
    }

    // Bump epoch to cancel any in-flight connect
    console.info('[Connection] disconnect: bumping epoch, dispatching USER_DISCONNECT');
    set((s) => ({ connectedTunnel: null, connectEpoch: s.connectEpoch + 1 }));
    vpnDispatch('USER_DISCONNECT');
    try {
      await window._k2.run('down');
    } catch (err) {
      console.error('[Connection] disconnect failed:', err);
    }
  },
}));

// ============ Lifecycle ============

export function initializeConnectionStore(): () => void {
  // Clear stale connectedTunnel when VPN reaches idle.
  // Covers: daemon restart, sleep/wake recovery, network change, serviceDown→recovery.
  // Idempotent with user-initiated disconnect() which pre-clears connectedTunnel.
  const unsub = useVPNMachineStore.subscribe(
    (s) => s.state,
    (state) => {
      if (state === 'idle') {
        const { connectedTunnel } = useConnectionStore.getState();
        if (connectedTunnel) {
          console.info('[Connection] VPN idle — clearing stale connectedTunnel');
          useConnectionStore.setState({ connectedTunnel: null });
        }
      }
    },
  );

  return unsub;
}
