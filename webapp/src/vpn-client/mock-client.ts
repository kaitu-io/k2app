import type { VpnClient, VpnStatus, VersionInfo, VpnConfig, ReadyState, VpnEvent } from './types';

export class MockVpnClient implements VpnClient {
  connectCalls: string[] = [];
  disconnectCalls = 0;

  private status: VpnStatus = { state: 'stopped' };
  private version: VersionInfo = { version: '0.0.0', go: '0.0', os: 'test', arch: 'test' };
  private ready: ReadyState = { ready: true, version: '0.0.0' };
  private config: VpnConfig = {};
  private udid = 'mock-device-id';
  private listeners: Set<(event: VpnEvent) => void> = new Set();
  private connectError: Error | null = null;
  private disconnectError: Error | null = null;

  setStatus(status: VpnStatus): void {
    this.status = status;
  }

  setVersion(version: VersionInfo): void {
    this.version = version;
  }

  setReady(ready: ReadyState): void {
    this.ready = ready;
  }

  setConnectError(error: Error | null): void {
    this.connectError = error;
  }

  setDisconnectError(error: Error | null): void {
    this.disconnectError = error;
  }

  simulateEvent(event: VpnEvent): void {
    for (const listener of this.listeners) {
      listener(event);
    }
  }

  async connect(wireUrl: string): Promise<void> {
    this.connectCalls.push(wireUrl);
    if (this.connectError) {
      throw this.connectError;
    }
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls++;
    if (this.disconnectError) {
      throw this.disconnectError;
    }
  }

  async checkReady(): Promise<ReadyState> {
    return this.ready;
  }

  async getStatus(): Promise<VpnStatus> {
    return this.status;
  }

  async getVersion(): Promise<VersionInfo> {
    return this.version;
  }

  async getUDID(): Promise<string> {
    return this.udid;
  }

  async getConfig(): Promise<VpnConfig> {
    return this.config;
  }

  subscribe(listener: (event: VpnEvent) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  destroy(): void {
    this.listeners.clear();
  }
}
