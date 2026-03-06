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
import { dispatch as vpnDispatch } from './vpn-machine.store';

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
    set({
      selectedSource: 'cloud',
      selectedCloudTunnel: tunnel,
      activeTunnel: computeCloudActiveTunnel(tunnel),
    });
  },

  selectSelfHosted: () => {
    set({
      selectedSource: 'self_hosted',
      activeTunnel: computeSelfHostedActiveTunnel(),
    });
  },

  connect: async () => {
    const { selectedSource, selectedCloudTunnel, activeTunnel, connectEpoch } = get();
    if (!activeTunnel) return;

    const myEpoch = connectEpoch + 1;
    set({ connectedTunnel: activeTunnel, connectEpoch: myEpoch });

    // Resolve server URL
    let serverUrl: string | undefined;
    if (selectedSource === 'self_hosted') {
      serverUrl = useSelfHostedStore.getState().tunnel?.uri;
    } else if (selectedCloudTunnel?.serverUrl) {
      serverUrl = await authService.buildTunnelUrl(selectedCloudTunnel.serverUrl);
    }

    // Epoch guard: bail if user disconnected or started new connect
    if (get().connectEpoch !== myEpoch) return;

    // Build config with explicit params
    const { buildConnectConfig, updateConfig } = useConfigStore.getState();
    const isBeta = window._platform?.updater?.channel === 'beta';
    const logLevel = localStorage.getItem('k2_log_level') || 'info';
    const config = buildConnectConfig({ serverUrl, isBeta, logLevel });

    // Persist BEFORE _k2.run so crash doesn't lose config
    await updateConfig({ server: serverUrl });

    // Dispatch state machine event and execute
    vpnDispatch('USER_CONNECT');
    try {
      await window._k2.run('up', config);
    } catch (err) {
      console.error('[ConnectionStore] connect failed:', err);
    }
  },

  disconnect: async () => {
    // Bump epoch to cancel any in-flight connect
    set((s) => ({ connectedTunnel: null, connectEpoch: s.connectEpoch + 1 }));
    vpnDispatch('USER_DISCONNECT');
    try {
      await window._k2.run('down');
    } catch (err) {
      console.error('[ConnectionStore] disconnect failed:', err);
    }
  },
}));
