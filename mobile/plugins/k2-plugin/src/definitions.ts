import type { PluginListenerHandle } from '@capacitor/core';

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

export interface K2PluginInterface {
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

  addListener(eventName: 'vpnStateChange', handler: (data: { state: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'vpnError', handler: (data: { message: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'updateDownloadProgress', handler: (data: { percent: number }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'nativeUpdateReady', handler: (data: { version: string; size: number; path: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'nativeUpdateAvailable', handler: (data: { version: string; url: string }) => void): Promise<PluginListenerHandle>;
}
