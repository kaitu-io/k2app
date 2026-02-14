import type { VpnClient, VpnStatus, VersionInfo, VpnConfig, ReadyState, VpnEvent, VpnState } from './types';

interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

export class HttpVpnClient implements VpnClient {
  private readonly baseUrl: string;
  private listeners: Set<(event: VpnEvent) => void> = new Set();
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private lastState: VpnState | null = null;

  constructor() {
    this.baseUrl = import.meta.env.DEV ? '' : 'http://127.0.0.1:1777';
  }

  async connect(wireUrl: string): Promise<void> {
    await this.coreRequest('up', { wire_url: wireUrl });
  }

  async disconnect(): Promise<void> {
    await this.coreRequest('down');
  }

  async getStatus(): Promise<VpnStatus> {
    const resp = await this.coreRequest<VpnStatus>('status');
    return resp.data ?? { state: 'stopped' };
  }

  async getVersion(): Promise<VersionInfo> {
    const resp = await this.coreRequest<VersionInfo>('version');
    return resp.data!;
  }

  async getUDID(): Promise<string> {
    const resp = await this.coreRequest<{ deviceId?: string; state: string }>('status');
    if (resp.data?.deviceId) {
      return resp.data.deviceId;
    }
    return crypto.randomUUID();
  }

  async getConfig(): Promise<VpnConfig> {
    const resp = await this.coreRequest<VpnConfig>('get_config');
    return resp.data ?? {};
  }

  async checkReady(): Promise<ReadyState> {
    try {
      const pingResp = await fetch(`${this.baseUrl}/ping`);
      if (!pingResp.ok) {
        return { ready: false, reason: 'not_running' };
      }
    } catch {
      return { ready: false, reason: 'not_running' };
    }

    try {
      const version = await this.getVersion();
      return { ready: true, version: version.version };
    } catch {
      return { ready: false, reason: 'version_mismatch' };
    }
  }

  subscribe(listener: (event: VpnEvent) => void): () => void {
    this.listeners.add(listener);
    this.startPolling();

    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.stopPolling();
      }
    };
  }

  destroy(): void {
    this.stopPolling();
    this.listeners.clear();
    this.lastState = null;
  }

  private startPolling(): void {
    if (this.pollTimer) return;

    const poll = async () => {
      try {
        const status = await this.getStatus();
        if (status.state !== this.lastState) {
          this.lastState = status.state;
          this.emit({ type: 'state_change', state: status.state });
        }
      } catch (e) {
        this.emit({ type: 'error', message: e instanceof Error ? e.message : 'Polling failed' });
      }
    };

    this.pollTimer = setInterval(() => {
      void poll();
    }, 2000);

    // Do an initial poll immediately
    void poll();
  }

  private stopPolling(): void {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
  }

  private emit(event: VpnEvent): void {
    for (const listener of this.listeners) {
      listener(event);
    }
  }

  private async coreRequest<T>(action: string, params?: Record<string, unknown>): Promise<ApiResponse<T>> {
    const body: Record<string, unknown> = { action };
    if (params) {
      body.params = params;
    }

    const resp = await fetch(`${this.baseUrl}/api/core`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    if (!resp.ok) {
      throw new Error(`Daemon API error: ${resp.status} ${resp.statusText}`);
    }

    return resp.json() as Promise<ApiResponse<T>>;
  }
}
