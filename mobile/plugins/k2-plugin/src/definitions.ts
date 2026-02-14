import type { PluginListenerHandle } from '@capacitor/core';

export interface K2PluginInterface {
  checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }>;
  getUDID(): Promise<{ udid: string }>;
  getVersion(): Promise<{ version: string; go: string; os: string; arch: string }>;
  getStatus(): Promise<{ state: string; connectedAt?: string; uptimeSeconds?: number; error?: string; wireUrl?: string }>;
  getConfig(): Promise<{ wireUrl?: string }>;
  connect(options: { wireUrl: string }): Promise<void>;
  disconnect(): Promise<void>;

  addListener(eventName: 'vpnStateChange', handler: (data: { state: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'vpnError', handler: (data: { message: string }) => void): Promise<PluginListenerHandle>;
}
