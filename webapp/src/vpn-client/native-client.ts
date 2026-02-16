import type { VpnClient, VpnStatus, VersionInfo, ClientConfig, ReadyState, VpnEvent, VpnState, UpdateCheckResult, WebUpdateInfo, NativeUpdateInfo } from './types';

// Keep in sync with mobile/plugins/k2-plugin/src/definitions.ts
interface K2PluginType {
  checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }>;
  getUDID(): Promise<{ udid: string }>;
  getVersion(): Promise<{ version: string; go: string; os: string; arch: string }>;
  getStatus(): Promise<{ state: string; connectedAt?: string; uptimeSeconds?: number; error?: string }>;
  getConfig(): Promise<{ config?: string }>;
  connect(options: { config: string }): Promise<void>;
  disconnect(): Promise<void>;
  checkWebUpdate(): Promise<WebUpdateInfo>;
  checkNativeUpdate(): Promise<NativeUpdateInfo>;
  applyWebUpdate(): Promise<void>;
  downloadNativeUpdate(): Promise<{ path: string }>;
  installNativeUpdate(options: { path: string }): Promise<void>;
  addListener(eventName: string, handler: (data: any) => void): Promise<{ remove: () => Promise<void> }>;
}

// Map engine state "disconnected" to webapp VpnState "stopped"
function mapState(state: string): VpnState {
  if (state === 'disconnected') return 'stopped';
  if (state === 'connecting') return 'connecting';
  if (state === 'connected') return 'connected';
  return 'stopped';
}

export class NativeVpnClient implements VpnClient {
  private listeners = new Set<(event: VpnEvent) => void>();
  private pluginListeners: Array<{ remove: () => Promise<void> }> = [];
  private pluginListenersInitialized = false;
  private plugin: K2PluginType;

  constructor(plugin: K2PluginType) {
    this.plugin = plugin;
  }

  async connect(config: ClientConfig): Promise<void> {
    await this.plugin.connect({ config: JSON.stringify(config) });
  }

  async disconnect(): Promise<void> {
    await this.plugin.disconnect();
  }

  async checkReady(): Promise<ReadyState> {
    const result = await this.plugin.checkReady();
    if (result.ready && result.version) {
      return { ready: true, version: result.version };
    }
    return { ready: false, reason: (result as any).reason || 'not_running' };
  }

  async getStatus(): Promise<VpnStatus> {
    const result = await this.plugin.getStatus();
    return {
      state: mapState(result.state),
      connectedAt: result.connectedAt,
      uptimeSeconds: result.uptimeSeconds,
      error: result.error,
    };
  }

  async getVersion(): Promise<VersionInfo> {
    return this.plugin.getVersion();
  }

  async getUDID(): Promise<string> {
    const { udid } = await this.plugin.getUDID();
    return udid;
  }

  async getConfig(): Promise<ClientConfig> {
    const result = await this.plugin.getConfig();
    if (result.config) {
      return JSON.parse(result.config) as ClientConfig;
    }
    return {} as ClientConfig;
  }

  subscribe(listener: (event: VpnEvent) => void): () => void {
    this.listeners.add(listener);

    if (!this.pluginListenersInitialized) {
      this.pluginListenersInitialized = true;
      // Set up plugin listeners on first subscription
      this.plugin.addListener('vpnStateChange', (data: { state: string }) => {
        const event: VpnEvent = { type: 'state_change', state: mapState(data.state) };
        this.listeners.forEach(l => l(event));
      }).then(handle => this.pluginListeners.push(handle));

      this.plugin.addListener('vpnError', (data: { message: string }) => {
        const event: VpnEvent = { type: 'error', message: data.message };
        this.listeners.forEach(l => l(event));
      }).then(handle => this.pluginListeners.push(handle));
    }

    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.pluginListeners.forEach(h => h.remove());
        this.pluginListeners = [];
        this.pluginListenersInitialized = false;
      }
    };
  }

  async applyWebUpdate(): Promise<void> {
    await this.plugin.applyWebUpdate();
  }

  async downloadNativeUpdate(): Promise<{ path: string }> {
    return this.plugin.downloadNativeUpdate();
  }

  async installNativeUpdate(options: { path: string }): Promise<void> {
    await this.plugin.installNativeUpdate(options);
  }

  onDownloadProgress(handler: (percent: number) => void): () => void {
    let handle: { remove: () => Promise<void> } | null = null;
    this.plugin.addListener('updateDownloadProgress', (data: { percent: number }) => {
      handler(data.percent);
    }).then(h => { handle = h; });
    return () => { handle?.remove(); };
  }

  async checkForUpdates(): Promise<UpdateCheckResult> {
    // Native update takes priority — may contain incompatible web changes
    try {
      const native = await this.plugin.checkNativeUpdate();
      if (native.available) {
        return {
          type: 'native',
          version: native.version,
          size: native.size,
          url: native.url,
        };
      }
    } catch {
      // Native check failed, continue to web check
    }

    try {
      const web = await this.plugin.checkWebUpdate();
      if (web.available) {
        return {
          type: 'web',
          version: web.version,
          size: web.size,
        };
      }
    } catch {
      // Web check failed
    }

    return { type: 'none' };
  }

  destroy(): void {
    this.pluginListeners.forEach(h => h.remove());
    this.pluginListeners = [];
    this.pluginListenersInitialized = false;
    this.listeners.clear();
  }
}
