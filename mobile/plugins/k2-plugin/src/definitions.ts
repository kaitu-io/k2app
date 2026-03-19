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

  appendLogs(options: { entries: Array<{ level: string; message: string; timestamp: number }> }): Promise<void>;
  uploadLogs(options: { email?: string; reason: string; feedbackId?: string; platform?: string; version?: string }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>;

  setLogLevel(options: { level: string }): Promise<void>;
  setDevEnabled(options: { enabled: boolean }): Promise<void>;
  debugDump(): Promise<Record<string, unknown>>;

  getUpdateChannel(): Promise<{ channel: string }>;
  setUpdateChannel(options: { channel: string }): Promise<{ channel: string }>;

  addListener(eventName: 'vpnStateChange', handler: (data: { state: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'vpnError', handler: (data: { message: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'updateDownloadProgress', handler: (data: { percent: number }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'nativeUpdateReady', handler: (data: { version: string; size: number; path: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'nativeUpdateAvailable', handler: (data: { version: string; url: string }) => void): Promise<PluginListenerHandle>;
}
