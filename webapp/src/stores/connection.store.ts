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
import type { Tunnel, TunnelListResponse } from '../services/api-types';
import { authService } from '../services/auth-service';
import { cacheStore } from '../services/cache-store';
import { cloudApi } from '../services/cloud-api';
import { useSelfHostedStore } from './self-hosted.store';
import { useConfigStore } from './config.store';
import { useAppRoutesStore } from './app-routes.store';
import { useVPNMachineStore, dispatch as vpnDispatch, type VPNState } from './vpn-machine.store';
import { useAuthStore } from './auth.store';
import { pickAutoTunnel } from '../utils/auto-tunnel-pick';
import { ERROR_CODES } from '../utils/errorCode';

// Minimum connection duration to surface the post-disconnect feedback dialog.
// Connections shorter than this are deemed too brief for a meaningful quality
// judgment, so the dialog is suppressed. Tune based on suppression-log volume.
const MIN_FEEDBACK_DURATION_SEC = 20;

// ============ Types ============

export interface ActiveTunnel {
  source: 'cloud' | 'self_hosted';
  domain: string;
  name: string;
  country: string;
  serverUrl: string;
  ipv4: string;
}

export interface LastConnectionInfo {
  domain: string;
  name: string;
  country: string;
  source: 'cloud' | 'self_hosted';
  durationSec: number;
  ruleMode: string;
  os: string;
  appVersion: string;
  commit: string;
}

interface ConnectionState {
  selectedCloudTunnel: Tunnel | null;
  activeTunnel: ActiveTunnel | null;
  connectedTunnel: ActiveTunnel | null;
  connectEpoch: number;
  connectedAt: number | null;
  feedbackRequested: boolean;
  pendingFeedback: boolean;
  lastConnectionInfo: LastConnectionInfo | null;
  /** Last k2v5 URL sent to the daemon (persisted, used for cold-start restore). */
  lastServerUrl: string | null;
  lastServerUrlLoaded: boolean;
  /** Server selection mode: 'manual' picks a specific cloud server; 'self_hosted' uses user's own node;
   *  'k2sub' (gateway only) lets the daemon resolve a `k2subs://` subscription URL. */
  serverMode: 'manual' | 'self_hosted' | 'k2sub';
  /** True once persisted serverMode has been loaded from storage. */
  serverModeLoaded: boolean;
  /** ISO 3166-1 alpha-2 country filter for k2subs subscription (lowercase), or null for auto. */
  subsCountry: string | null;
  /** True once persisted subsCountry has been loaded from storage. */
  subsCountryLoaded: boolean;
  /**
   * True when the cloud tunnel endpoint last reported 402 (membership expired).
   * This is the live entitlement signal — fresher than `useUser().isExpired` —
   * and the single source of truth that empties the cloud list and disables the
   * connect button in manual mode. Cleared automatically on the next successful
   * tunnel load (membership renewed). Set via `setCloudAccess()`.
   */
  cloudAccessRevoked: boolean;
}

// Persisted last-used server URL. Kept separate from config.store because
// ClientConfig mirrors the Go wire contract, which has no `server` field.
const LAST_SERVER_URL_STORAGE_KEY = 'k2.vpn.last_server_url';
const SERVER_MODE_STORAGE_KEY = 'k2.vpn.server_mode';
const SUBS_COUNTRY_STORAGE_KEY = 'k2.connection.subsCountry';

/**
 * Canonical "Auto pick" sentinel domain.
 *
 * Exported so UI props that flow as `selectedDomain: string | null` (e.g.,
 * CloudTunnelList) can compare against this constant instead of repeating
 * the literal `'__auto__'`.
 */
export const AUTO_TUNNEL_DOMAIN = '__auto__';

/**
 * Module-level sentinel Tunnel representing "Auto pick mode".
 *
 * Identity is by reference equality (use `isAutoSelection`). Field values
 * are inert and intentionally unusable: serverUrl is empty so the sentinel
 * can never be passed to `_k2.run('up')` by accident. The `connect()` path
 * resolves the sentinel into a concrete Tunnel via `pickAutoTunnel` before
 * any call into the bridge.
 */
export const AUTO_TUNNEL_SENTINEL: Readonly<Tunnel> = Object.freeze({
  id: -1,
  domain: AUTO_TUNNEL_DOMAIN,
  name: 'Auto',
  protocol: 'k2v5',
  port: 0,
  serverUrl: '',
  node: Object.freeze({
    name: '',
    country: '',
    region: '',
    ipv4: '',
    ipv6: '',
    isAlive: false,
    load: 0,
    trafficUsagePercent: 0,
    bandwidthUsagePercent: 0,
  }),
  recommendScore: 0,
});

/** True when `t` is the Auto sentinel (reference equality). */
export function isAutoSelection(t: Tunnel | null): boolean {
  return t === AUTO_TUNNEL_SENTINEL;
}

// Auto pick triggers a fire-and-forget tunnel refresh so the *next* pick uses
// fresher recommendScores. Polled so the request fires only once the VPN
// machine is stable (connected/idle/serviceDown) — avoids racing the active
// connect, which would compete with TUN/route setup.
const AUTO_PICK_REFRESH_POLL_MS = 1000;
const AUTO_PICK_REFRESH_MAX_WAIT_MS = 30000;

const STABLE_STATES: ReadonlySet<VPNState> = new Set(['connected', 'idle', 'serviceDown']);

export function refreshTunnelsCacheAfterAutoPick(
  opts?: { pollIntervalMs?: number; maxWaitMs?: number },
): void {
  const pollMs = opts?.pollIntervalMs ?? AUTO_PICK_REFRESH_POLL_MS;
  const maxMs = opts?.maxWaitMs ?? AUTO_PICK_REFRESH_MAX_WAIT_MS;
  const startedAt = Date.now();

  const attempt = (): void => {
    const state = useVPNMachineStore.getState().state;
    if (STABLE_STATES.has(state)) {
      cloudApi.get<TunnelListResponse>('/api/tunnels/k2v4').then(res => {
        if (res.code === 0 && res.data) {
          cacheStore.set('api:tunnels', res.data);
          console.debug('[Connection] auto-pick: tunnel cache refreshed ('
            + (res.data.items?.length ?? 0) + ' items, vpnState=' + state + ')');
        } else if (res.code !== 401) {
          console.warn('[Connection] auto-pick: tunnel refresh failed code=' + res.code);
        }
      }).catch(err => {
        console.warn('[Connection] auto-pick: tunnel refresh error', err);
      });
      return;
    }
    if (Date.now() - startedAt >= maxMs) {
      console.warn('[Connection] auto-pick: tunnel refresh skipped, vpnState=' + state
        + ' did not stabilize within ' + maxMs + 'ms');
      return;
    }
    setTimeout(attempt, pollMs);
  };

  setTimeout(attempt, pollMs);
}

async function persistLastServerUrl(url: string | null): Promise<void> {
  try {
    if (url) {
      await window._platform.storage.set(LAST_SERVER_URL_STORAGE_KEY, url);
    } else {
      await window._platform.storage.remove(LAST_SERVER_URL_STORAGE_KEY);
    }
  } catch (err) {
    console.warn('[Connection] Failed to persist last server URL:', err);
  }
}

async function persistServerMode(mode: 'manual' | 'self_hosted' | 'k2sub'): Promise<void> {
  try {
    await window._platform.storage.set(SERVER_MODE_STORAGE_KEY, mode);
  } catch (err) {
    console.warn('[Connection] Failed to persist serverMode:', err);
  }
}

interface ConnectionActions {
  selectCloudTunnel: (tunnel: Tunnel) => void;
  clearCloudSelection: () => void;
  reconcileSelection: (tunnels: Tunnel[]) => void;
  /**
   * Reflect cloud-tunnel entitlement from a tunnel-list fetch outcome.
   * - `false` (402 / membership expired): purge the tunnel cache, drop any
   *   cloud selection, and raise `cloudAccessRevoked` so the connect button
   *   goes inert in manual mode. Idempotent.
   * - `true` (a fetch returned tunnels): lower the flag if it was set.
   */
  setCloudAccess: (available: boolean) => void;
  selectSelfHosted: () => void;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  clearPendingFeedback: () => void;
  enrichFromTunnelList: (tunnels: Tunnel[]) => void;
  setServerMode: (mode: 'manual' | 'self_hosted' | 'k2sub') => Promise<void>;
  loadServerMode: () => Promise<void>;
  setSubsCountry: (country: string | null) => Promise<void>;
  loadSubsCountry: () => Promise<void>;
}

// ============ Helpers ============

/** Extract hostname (tunnel domain) from k2v5://udid:token@host:port?... URL */
function extractDomainFromServerUrl(serverUrl: string): string | null {
  try {
    const url = new URL(serverUrl.replace(/^k2v\d+:\/\//, 'https://'));
    return url.hostname.toLowerCase() || null;
  } catch {
    return null;
  }
}

function computeCloudActiveTunnel(tunnel: Tunnel): ActiveTunnel {
  return {
    source: 'cloud',
    domain: tunnel.domain.toLowerCase(),
    name: tunnel.name || tunnel.domain,
    country: tunnel.node?.country || '',
    serverUrl: tunnel.serverUrl || '',
    ipv4: tunnel.node?.ipv4 ?? '',
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
    ipv4: '',
  };
}

// ============ Store ============

export const useConnectionStore = create<ConnectionState & ConnectionActions>()((set, get) => ({
  // State
  selectedCloudTunnel: null,
  activeTunnel: null,
  connectedTunnel: null,
  connectEpoch: 0,
  connectedAt: null,
  feedbackRequested: false,
  pendingFeedback: false,
  lastConnectionInfo: null,
  lastServerUrl: null,
  lastServerUrlLoaded: false,
  serverMode: 'manual',
  serverModeLoaded: false,
  subsCountry: null,
  subsCountryLoaded: false,
  cloudAccessRevoked: false,

  // Actions
  selectCloudTunnel: (tunnel) => {
    console.info('[Connection] selectCloudTunnel: domain=' + tunnel.domain + ', name=' + (tunnel.name || tunnel.domain));
    // Picking a specific cloud tunnel implies manual mode — symmetric with selectSelfHosted.
    set({
      selectedCloudTunnel: tunnel,
      activeTunnel: computeCloudActiveTunnel(tunnel),
      serverMode: 'manual',
    });
    void persistServerMode('manual');
  },

  clearCloudSelection: () => {
    console.info('[Connection] clearCloudSelection (→ Auto via derivation)');
    set({
      selectedCloudTunnel: null,
      activeTunnel: null,
      serverMode: 'manual',
    });
    void persistServerMode('manual');
  },

  reconcileSelection: (tunnels) => {
    const { selectedCloudTunnel } = get();
    if (!selectedCloudTunnel || isAutoSelection(selectedCloudTunnel)) return;
    const stillExists = tunnels.some(t => t.domain === selectedCloudTunnel.domain);
    if (!stillExists) {
      console.info('[Connection] selected tunnel ' + selectedCloudTunnel.domain
        + ' offline, falling back to Auto');
      set({ selectedCloudTunnel: null, activeTunnel: null });
    }
  },

  setCloudAccess: (available) => {
    if (available) {
      // Membership active — lower the flag if it was raised. Cheap no-op otherwise.
      if (get().cloudAccessRevoked) {
        console.info('[Connection] cloud access restored');
        set({ cloudAccessRevoked: false });
      }
      return;
    }
    // Membership expired (402). Idempotent — only act on the first observation.
    if (get().cloudAccessRevoked) return;
    console.warn('[Connection] cloud access revoked (402) — clearing cloud selection + tunnel cache');
    cacheStore.delete('api:tunnels');
    set({ selectedCloudTunnel: null, activeTunnel: null, cloudAccessRevoked: true });
  },

  selectSelfHosted: () => {
    const tunnel = computeSelfHostedActiveTunnel();
    console.info('[Connection] selectSelfHosted: domain=' + (tunnel?.domain ?? 'null'));
    set({
      serverMode: 'self_hosted',
      activeTunnel: tunnel,
    });
    void persistServerMode('self_hosted');
  },

  setServerMode: async (mode: 'manual' | 'self_hosted' | 'k2sub') => {
    if (mode === 'self_hosted') {
      const tunnel = computeSelfHostedActiveTunnel();
      set({ serverMode: mode, activeTunnel: tunnel });
    } else {
      set({ serverMode: mode });
    }
    await persistServerMode(mode);
  },

  loadServerMode: async () => {
    const isGateway = window._platform?.platformType === 'gateway';
    try {
      const stored = await window._platform.storage.get<string>(SERVER_MODE_STORAGE_KEY);
      let resolved: 'manual' | 'self_hosted' | 'k2sub';
      if (stored === 'self_hosted') {
        resolved = 'self_hosted';
      } else if (isGateway) {
        // Gateway default = k2sub. Coerce any other value (incl. legacy 'manual', 'smart') to k2sub.
        resolved = 'k2sub';
      } else {
        // Non-gateway: 'k2sub' is gateway-only — coerce to manual. Same for legacy 'smart'.
        resolved = 'manual';
      }
      useConnectionStore.setState({
        serverMode: resolved,
        serverModeLoaded: true,
      });
      if (stored !== resolved) {
        void persistServerMode(resolved);
      }
    } catch (err) {
      console.warn('[Connection] Failed to load serverMode:', err);
      useConnectionStore.setState({ serverModeLoaded: true });
    }
  },

  setSubsCountry: async (country: string | null) => {
    set({ subsCountry: country });
    try {
      if (country === null) {
        await window._platform.storage.remove(SUBS_COUNTRY_STORAGE_KEY);
      } else {
        await window._platform.storage.set(SUBS_COUNTRY_STORAGE_KEY, country);
      }
    } catch (err) {
      console.warn('[Connection] Failed to persist subsCountry:', err);
    }
  },

  loadSubsCountry: async () => {
    try {
      const v = await window._platform.storage.get<string>(SUBS_COUNTRY_STORAGE_KEY);
      useConnectionStore.setState({
        subsCountry: typeof v === 'string' && v ? v : null,
        subsCountryLoaded: true,
      });
    } catch (err) {
      console.warn('[Connection] Failed to load subsCountry:', err);
      useConnectionStore.setState({ subsCountryLoaded: true });
    }
  },

  connect: async () => {
    const t0 = Date.now();
    // State guard: reject if already connecting/connected/reconnecting/disconnecting.
    // Prevents double-click sending duplicate _k2.run('up') — daemon's opMu serializes
    // but would cause unnecessary disconnect+reconnect cycle.
    const vpnState = useVPNMachineStore.getState().state;
    console.warn('[Connection] TRACE connect START t=' + t0 + ' vpnState=' + vpnState);
    if (vpnState !== 'idle' && vpnState !== 'serviceDown') {
      console.warn('[Connection] connect: rejected (vpnState=' + vpnState + ')');
      return;
    }

    const { selectedCloudTunnel, connectEpoch, serverMode } = get();

    // Resolve Auto sentinel (manual mode + null selection) into a concrete Tunnel.
    let resolvedTunnel: Tunnel | null = selectedCloudTunnel;
    if (serverMode === 'manual' && selectedCloudTunnel === null) {
      const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
      const tunnelList = cached?.items ?? [];
      resolvedTunnel = pickAutoTunnel(tunnelList);
      if (!resolvedTunnel) {
        console.warn('[Connection] connect: Auto mode but no tunnel available, aborting');
        vpnDispatch('BACKEND_ERROR', {
          error: { code: ERROR_CODES.NO_TUNNEL_AVAILABLE_AUTO, message: 'No tunnel available for auto pick' },
          isRetrying: false,
        });
        return;
      }
      console.info('[Connection] auto-pick → ' + resolvedTunnel.domain
        + ' (score=' + resolvedTunnel.recommendScore + ')');
      refreshTunnelsCacheAfterAutoPick();
    }

    // Pre-flight mode/selection validity. Tightened to reject empty-string uri/serverUrl
    // (stale tunnel list, externally wiped self-hosted config), not just null, so we never
    // send an empty-routes payload to the daemon.
    if (serverMode === 'self_hosted' && !useSelfHostedStore.getState().tunnel?.uri) {
      console.warn('[Connection] connect: self_hosted mode but no tunnel URI, aborting');
      vpnDispatch('BACKEND_ERROR', {
        error: { code: 400, message: 'No self-hosted tunnel configured' },
        isRetrying: false,
      });
      return;
    }

    if (serverMode === 'manual' && !resolvedTunnel?.serverUrl) {
      console.warn('[Connection] connect: manual mode but resolved tunnel has no serverUrl, aborting');
      vpnDispatch('BACKEND_ERROR', {
        error: { code: 400, message: 'No server selected' },
        isRetrying: false,
      });
      return;
    }

    const myEpoch = connectEpoch + 1;

    // Build connectedTunnel snapshot for UI display.
    const selfHostedSnap = serverMode === 'self_hosted' ? computeSelfHostedActiveTunnel() : null;
    let connectedTunnelSnapshot: ActiveTunnel | null;
    if (serverMode === 'self_hosted') {
      connectedTunnelSnapshot = selfHostedSnap
        ?? { source: 'self_hosted', domain: 'self_hosted', name: '自部署', country: '', serverUrl: '', ipv4: '' };
    } else if (serverMode === 'k2sub') {
      const { subsCountry } = get();
      connectedTunnelSnapshot = {
        source: 'cloud',
        domain: 'k2sub',
        name: subsCountry ? subsCountry.toUpperCase() : 'Auto',
        country: subsCountry ?? '',
        serverUrl: '',
        ipv4: '',
      };
    } else {
      connectedTunnelSnapshot = resolvedTunnel ? computeCloudActiveTunnel(resolvedTunnel) : null;
    }
    if (!connectedTunnelSnapshot) {
      // Pre-flight checks above catch manual-mode-without-tunnel.
      return;
    }

    console.info(
      '[Connection] connect: mode=' + serverMode
      + (serverMode === 'self_hosted'
        ? ', uri=' + (selfHostedSnap?.serverUrl ?? 'none')
        : ', tunnel=' + (resolvedTunnel?.domain ?? 'none'))
      + ', epoch=' + connectEpoch + '→' + myEpoch,
    );
    set({ connectedTunnel: connectedTunnelSnapshot, connectEpoch: myEpoch, connectedAt: Date.now() });
    console.warn('[Connection] TRACE connectedTunnel set t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Resolve server URL
    let serverUrl: string | undefined;
    if (serverMode === 'self_hosted') {
      serverUrl = useSelfHostedStore.getState().tunnel?.uri;
    } else if (serverMode === 'k2sub') {
      const { subsCountry } = get();
      serverUrl = await authService.buildSubsUrl(subsCountry);
    } else if (resolvedTunnel?.serverUrl) {
      serverUrl = await authService.buildTunnelUrl(resolvedTunnel.serverUrl);
    }
    console.warn('[Connection] TRACE buildTunnelUrl done t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Epoch guard: bail if user disconnected or started new connect
    if (get().connectEpoch !== myEpoch) {
      console.warn('[Connection] connect: epoch mismatch (mine=' + myEpoch + ', current=' + get().connectEpoch + '), aborting');
      return;
    }

    // Post-resolve hard guard: an empty/invalid serverUrl would produce an empty routes[]
    // in buildConnectConfig, which the daemon silently no-ops (no engine.Start logs, no
    // error returned to the webapp). Abort with a user-visible error instead.
    const invalidServerUrl = !serverUrl;
    if (invalidServerUrl) {
      console.error('[Connection] connect: invalid serverUrl for mode=' + serverMode
        + ', serverUrl=' + (serverUrl ?? 'undefined')
        + ' — aborting before _k2.run(up)');
      vpnDispatch('BACKEND_ERROR', {
        error: { code: 400, message: 'Invalid server URL' },
        isRetrying: false,
      });
      return;
    }

    // Build config with explicit params
    const { buildConnectConfig, resolvePreset, country: configCountry, alwaysOn } = useConfigStore.getState();
    const { forceDirect, forceProxy } = useAppRoutesStore.getState();
    const config = buildConnectConfig({ serverUrl, forceDirect, forceProxy });
    const currentPreset = resolvePreset();
    console.debug('[Connection] connect: config built, preset=' + currentPreset
      + ', country=' + (configCountry ?? 'null')
      + ', routes=' + (config.routes?.length ?? 0)
      + ', serverUrl=' + (serverUrl ?? 'none')
      + ', logLevel=' + config.log?.level
      + ', alwaysOn=' + alwaysOn);

    // Persist BEFORE _k2.run so crash doesn't lose the tunnel identity used
    // by cold-start restore.
    if (serverUrl) {
      set({ lastServerUrl: serverUrl });
      await persistLastServerUrl(serverUrl);
    }
    console.warn('[Connection] TRACE persistLastServerUrl done t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');

    // Dispatch state machine event and execute
    console.warn('[Connection] TRACE USER_CONNECT dispatch t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms)');
    vpnDispatch('USER_CONNECT');
    try {
      const resp = await window._k2.run('up', { config, alwaysOn });
      console.warn('[Connection] TRACE _k2.run(up) returned t=' + Date.now() + ' (+' + (Date.now() - t0) + 'ms) code=' + resp.code);


      // If the connect call itself failed (e.g. VPN permission denied),
      // dispatch BACKEND_ERROR so the state machine exits 'connecting'.
      if (resp.code !== 0) {
        const errorCode = resp.code > 0 ? resp.code : 570;
        vpnDispatch('BACKEND_ERROR', {
          error: { code: errorCode, message: resp.message || 'Connect failed' },
          isRetrying: false,
        });
      }
    } catch (err) {
      console.error('[Connection] connect failed:', err);
      vpnDispatch('BACKEND_ERROR', {
        error: { code: 570, message: err instanceof Error ? err.message : String(err) },
        isRetrying: false,
      });
    }
  },

  enrichFromTunnelList: (tunnels) => {
    const { connectedTunnel } = get();
    if (!connectedTunnel || connectedTunnel.source !== 'cloud') return;
    if (connectedTunnel.country) return; // Already enriched, idempotent

    const match = tunnels.find(t => t.domain.toLowerCase() === connectedTunnel.domain);
    if (!match) return;

    const enriched = computeCloudActiveTunnel(match);
    console.info('[Connection] Enriched from tunnel list: domain=' + match.domain
      + ', name=' + (match.name || match.domain) + ', country=' + (match.node?.country || ''));
    set({
      connectedTunnel: enriched,
      activeTunnel: enriched,
    });
  },

  disconnect: async () => {
    const vpnState = useVPNMachineStore.getState().state;
    if (vpnState === 'disconnecting' || vpnState === 'idle') {
      console.warn('[Connection] disconnect: rejected (vpnState=' + vpnState + ')');
      return;
    }

    // Snapshot connection info BEFORE clearing connectedTunnel
    const { connectedTunnel, connectedAt } = get();
    const isAuthenticated = useAuthStore.getState().isAuthenticated;
    let lastConnectionInfo: LastConnectionInfo | null = null;

    if (connectedTunnel && isAuthenticated) {
      const configState = useConfigStore.getState();
      const disconnectPreset = configState.resolvePreset();
      // Source-of-truth chain for session start: engine startAt → webapp connectedAt → 0.
      // Engine startAt survives webapp cold-start (e.g., iOS background-reload),
      // which connectedAt does not — see tryRestoreConnectedTunnel below.
      // Math.max(0, ...) guards against transient NTP backward jumps.
      const startAt = useVPNMachineStore.getState().startAt;
      const sessionStartMs = startAt ? startAt * 1000 : connectedAt;
      const durationSec = sessionStartMs
        ? Math.max(0, Math.round((Date.now() - sessionStartMs) / 1000))
        : 0;
      lastConnectionInfo = {
        domain: connectedTunnel.domain,
        name: connectedTunnel.name,
        country: connectedTunnel.country,
        source: connectedTunnel.source,
        durationSec,
        ruleMode: disconnectPreset === 'global' ? 'global' : (configState.country ?? 'split'),
        os: window._platform?.os || 'unknown',
        appVersion: window._platform?.version || '0.0.0',
        commit: window._platform?.commit || '',
      };
    }

    const shouldRequestFeedback = !!lastConnectionInfo && lastConnectionInfo.durationSec >= MIN_FEEDBACK_DURATION_SEC;
    if (lastConnectionInfo && !shouldRequestFeedback) {
      console.info(
        `[Connection] feedback dialog suppressed: domain=${lastConnectionInfo.domain} durationSec=${lastConnectionInfo.durationSec}s`,
      );
    }
    console.info('[Connection] disconnect: bumping epoch, dispatching USER_DISCONNECT');
    set((s) => ({
      connectedTunnel: null,
      connectedAt: null,
      connectEpoch: s.connectEpoch + 1,
      feedbackRequested: shouldRequestFeedback,
      lastConnectionInfo: shouldRequestFeedback ? lastConnectionInfo : null,
      lastServerUrl: null,
    }));
    // Fire-and-forget: persisted identity must not outlive the user's intent.
    persistLastServerUrl(null);
    vpnDispatch('USER_DISCONNECT');
    try {
      await window._k2.run('down');
    } catch (err) {
      console.error('[Connection] disconnect failed:', err);
    }
  },

  clearPendingFeedback: () => {
    set({ pendingFeedback: false, lastConnectionInfo: null });
  },
}));

// ============ Cold Start Recovery ============

/**
 * Cold start restore: when VPN is active but connectedTunnel is lost (app process killed),
 * recover from persisted `lastServerUrl` + self-hosted store.
 *
 * Guards: lastServerUrlLoaded && selfHostedLoaded && vpnActive && !connectedTunnel
 * Normal connect flow sets connectedTunnel in connect() before VPN activates, so guards skip.
 *
 * Three async data sources (lastServerUrl / selfHosted / vpnState) complete in unknown order.
 * Three independent subscriptions trigger this; guards ensure execution only when all ready,
 * and connectedTunnel guard ensures at-most-once execution.
 */
function tryRestoreConnectedTunnel(): boolean {
  const vpnState = useVPNMachineStore.getState().state;
  const { connectedTunnel, lastServerUrl, lastServerUrlLoaded, serverMode, serverModeLoaded } = useConnectionStore.getState();
  const { loaded: selfHostedLoaded } = useSelfHostedStore.getState();

  if (!lastServerUrlLoaded) return false;
  if (!selfHostedLoaded) return false;
  if (!serverModeLoaded) return false;
  if (connectedTunnel) return false;
  if (vpnState !== 'connected' && vpnState !== 'connecting' && vpnState !== 'reconnecting') return false;

  // Self-hosted tab mode: restore from current self-hosted tunnel config.
  if (serverMode === 'self_hosted') {
    const shTunnel = useSelfHostedStore.getState().tunnel;
    if (!shTunnel) return false;
    const activeTunnel = computeSelfHostedActiveTunnel();
    console.info('[Connection] Cold start restore: self_hosted tab, uri=' + shTunnel.uri);
    useConnectionStore.setState({ serverMode: 'self_hosted', connectedTunnel: activeTunnel, activeTunnel });
    return true;
  }

  // Manual mode: existing URL-based restore.
  const serverUrl = lastServerUrl;
  if (!serverUrl) return false;

  const domain = extractDomainFromServerUrl(serverUrl);
  if (!domain) return false;

  // Check self-hosted first (full info already persisted)
  const selfHosted = useSelfHostedStore.getState().tunnel;
  if (selfHosted) {
    const selfHostedDomain = extractDomainFromServerUrl(selfHosted.uri);
    if (selfHostedDomain === domain) {
      const activeTunnel = computeSelfHostedActiveTunnel();
      console.info('[Connection] Cold start restore: self-hosted domain=' + domain);
      useConnectionStore.setState({
        serverMode: 'self_hosted',
        connectedTunnel: activeTunnel,
        activeTunnel,
      });
      return true;
    }
  }

  // Cloud: set domain-only partial restore. Dashboard useEffect handles enrichment
  // from cacheStore (works in both cold start and warm start).
  console.info('[Connection] Cold start restore: cloud domain=' + domain + ' (pending enrichment)');
  useConnectionStore.setState({
    connectedTunnel: {
      source: 'cloud',
      domain,
      name: domain,
      country: '',
      serverUrl,
      ipv4: '',
    },
  });
  return true;
}

// ============ Lifecycle ============

export function initializeConnectionStore(): () => void {
  // Load persisted lastServerUrl (fire-and-forget). When done, triggers
  // tryRestoreConnectedTunnel via the store subscription below.
  (async () => {
    try {
      const stored = await window._platform.storage.get<string>(LAST_SERVER_URL_STORAGE_KEY);
      useConnectionStore.setState({
        lastServerUrl: typeof stored === 'string' && stored ? stored : null,
        lastServerUrlLoaded: true,
      });
    } catch (err) {
      console.warn('[Connection] Failed to load last server URL:', err);
      useConnectionStore.setState({ lastServerUrl: null, lastServerUrlLoaded: true });
    }
  })();

  // Load persisted serverMode (fire-and-forget).
  // When done, triggers tryRestoreConnectedTunnel via Trigger 2.
  useConnectionStore.getState().loadServerMode();

  // Load persisted subsCountry (fire-and-forget). Independent of cold-start restore.
  useConnectionStore.getState().loadSubsCountry();

  // Trigger 1: VPN state changes (vpn-machine.store uses subscribeWithSelector middleware)
  const unsubVPN = useVPNMachineStore.subscribe(
    (s) => s.state,
    (state) => {
      // Clear stale connectedTunnel when VPN reaches idle.
      // BACKEND_ERROR now routes to idle (non-retrying) or reconnecting (retrying),
      // so this single check covers both normal disconnect and error cases.
      if (state === 'idle') {
        const { connectedTunnel, feedbackRequested } = useConnectionStore.getState();
        const updates: Partial<ConnectionState> = {};
        if (connectedTunnel) {
          console.info('[Connection] VPN idle — clearing connectedTunnel');
          updates.connectedTunnel = null;
        }
        if (feedbackRequested) {
          console.info('[Connection] VPN idle — promoting feedbackRequested → pendingFeedback');
          updates.feedbackRequested = false;
          updates.pendingFeedback = true;
        }
        if (Object.keys(updates).length > 0) {
          useConnectionStore.setState(updates);
        }
      }
      // Cold start recovery: VPN active but connectedTunnel lost
      if (state === 'connected' || state === 'connecting' || state === 'reconnecting') {
        if (!useConnectionStore.getState().connectedTunnel) {
          tryRestoreConnectedTunnel();
        }
      }
    },
  );

  // Trigger 2: lastServerUrl or serverMode loaded (either may arrive after VPN status)
  const unsubConnection = useConnectionStore.subscribe((state, prevState) => {
    if (state.lastServerUrlLoaded && !prevState.lastServerUrlLoaded) {
      tryRestoreConnectedTunnel();
    }
    if (state.serverModeLoaded && !prevState.serverModeLoaded) {
      tryRestoreConnectedTunnel();
    }
  });

  // Trigger 3: selfHosted loaded (must be ready before self-hosted vs cloud determination)
  const unsubSelfHosted = useSelfHostedStore.subscribe((state, prevState) => {
    if (state.loaded && !prevState.loaded) {
      tryRestoreConnectedTunnel();
    }
  });

  return () => {
    unsubVPN();
    unsubConnection();
    unsubSelfHosted();
  };
}

/**
 * Returns the effective cloud tunnel selection for UI consumption.
 *
 * - Returns AUTO_TUNNEL_SENTINEL when serverMode='manual' and no concrete
 *   tunnel is selected (the default state — Auto is selected).
 * - Returns the concrete selected tunnel when one is chosen.
 * - Returns null in self_hosted mode (cloud selection does not apply).
 *
 * UI components should use this hook rather than reading `selectedCloudTunnel`
 * directly, so the Auto default surfaces consistently in the list and top card.
 */
export function useEffectiveCloudSelection(): Tunnel | null {
  return useConnectionStore((s) => {
    if (s.serverMode !== 'manual') return null;
    return s.selectedCloudTunnel ?? AUTO_TUNNEL_SENTINEL;
  });
}

/**
 * Pure predicate: is the user's current selection ready to initiate a connect?
 *
 * Each serverMode has its own readiness rule:
 *   - manual: Auto is the always-fallback. Ready when a concrete tunnel is
 *     active OR no concrete pick (Auto sentinel default).
 *   - self_hosted: requires a configured tunnel (activeTunnel set).
 *   - k2sub: subsCountry===null is the always-fallback (Auto). The daemon
 *     resolves the k2subs:// URL on connect, so the UI is always ready.
 */
export function hasConnectableSelection(
  s: Pick<ConnectionState, 'serverMode' | 'activeTunnel' | 'selectedCloudTunnel' | 'cloudAccessRevoked'>,
): boolean {
  if (s.serverMode === 'self_hosted') return !!s.activeTunnel;
  if (s.serverMode === 'k2sub') return true;
  // Manual mode draws on cloud tunnels — if membership lapsed (402), there is
  // nothing to connect to, so the Auto fallback is no longer connectable.
  if (s.cloudAccessRevoked) return false;
  return !!s.activeTunnel || s.selectedCloudTunnel === null;
}

/** Hook wrapper around `hasConnectableSelection` for component consumers. */
export function useHasConnectableSelection(): boolean {
  return useConnectionStore(hasConnectableSelection);
}
