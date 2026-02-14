export type VpnState = 'stopped' | 'connecting' | 'connected';

export interface VpnStatus {
  state: VpnState;
  connectedAt?: string;
  uptimeSeconds?: number;
  error?: string;
  wireUrl?: string;
}

export interface VersionInfo {
  version: string;
  go: string;
  os: string;
  arch: string;
}

export interface VpnConfig {
  wireUrl?: string;
  configPath?: string;
}

export type ReadyState =
  | { ready: true; version: string }
  | { ready: false; reason: 'not_running' | 'version_mismatch' | 'not_installed' };

export type VpnEvent =
  | { type: 'state_change'; state: VpnState }
  | { type: 'error'; message: string };

export interface VpnClient {
  connect(wireUrl: string): Promise<void>;
  disconnect(): Promise<void>;
  checkReady(): Promise<ReadyState>;
  getStatus(): Promise<VpnStatus>;
  getVersion(): Promise<VersionInfo>;
  getUDID(): Promise<string>;
  getConfig(): Promise<VpnConfig>;
  subscribe(listener: (event: VpnEvent) => void): () => void;
  destroy(): void;
}
