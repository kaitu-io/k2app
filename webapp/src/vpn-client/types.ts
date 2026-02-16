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

export interface ClientConfig {
  server: string;
  mode?: string;
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
  rule?: { global?: boolean };
  log?: { level?: string };
}

export type ReadyState =
  | { ready: true; version: string }
  | { ready: false; reason: 'not_running' | 'version_mismatch' | 'not_installed' };

export interface WebUpdateInfo {
  available: boolean;
  version?: string;
  size?: number;
}

export interface NativeUpdateInfo {
  available: boolean;
  version?: string;
  size?: number;
  url?: string;
}

export interface UpdateCheckResult {
  type: 'native' | 'web' | 'none';
  version?: string;
  size?: number;
  url?: string;
}

export type VpnEvent =
  | { type: 'state_change'; state: VpnState }
  | { type: 'error'; message: string }
  | { type: 'download_progress'; percent: number };

export interface VpnClient {
  connect(config: ClientConfig): Promise<void>;
  disconnect(): Promise<void>;
  checkReady(): Promise<ReadyState>;
  getStatus(): Promise<VpnStatus>;
  getVersion(): Promise<VersionInfo>;
  getUDID(): Promise<string>;
  getConfig(): Promise<ClientConfig>;
  subscribe(listener: (event: VpnEvent) => void): () => void;
  destroy(): void;
  checkForUpdates?(): Promise<UpdateCheckResult>;
  applyWebUpdate?(): Promise<void>;
  downloadNativeUpdate?(): Promise<{ path: string }>;
  installNativeUpdate?(options: { path: string }): Promise<void>;
  onDownloadProgress?(handler: (percent: number) => void): () => void;
}
