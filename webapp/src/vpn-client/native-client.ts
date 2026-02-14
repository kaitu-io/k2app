import type { VpnClient, VpnStatus, VersionInfo, VpnConfig, ReadyState, VpnEvent, VpnState } from './types';

// K2Plugin is imported dynamically. This type represents the plugin interface.
interface K2PluginType {
  checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }>;
  getUDID(): Promise<{ udid: string }>;
  getVersion(): Promise<{ version: string; go: string; os: string; arch: string }>;
  getStatus(): Promise<{ state: string; connectedAt?: string; uptimeSeconds?: number; error?: string; wireUrl?: string }>;
  getConfig(): Promise<{ wireUrl?: string }>;
  connect(options: { wireUrl: string }): Promise<void>;
  disconnect(): Promise<void>;
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

  async connect(wireUrl: string): Promise<void> {
    await this.plugin.connect({ wireUrl });
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
      wireUrl: result.wireUrl,
    };
  }

  async getVersion(): Promise<VersionInfo> {
    return this.plugin.getVersion();
  }

  async getUDID(): Promise<string> {
    const { udid } = await this.plugin.getUDID();
    return udid;
  }

  async getConfig(): Promise<VpnConfig> {
    return this.plugin.getConfig();
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

  destroy(): void {
    this.pluginListeners.forEach(h => h.remove());
    this.pluginListeners = [];
    this.pluginListenersInitialized = false;
    this.listeners.clear();
  }
}
