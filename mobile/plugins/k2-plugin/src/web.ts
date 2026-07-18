import { WebPlugin } from '@capacitor/core';

import type { K2PluginInterface, WebUpdateInfo, NativeUpdateInfo } from './definitions';

export class K2PluginWeb extends WebPlugin implements K2PluginInterface {
  async checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getVersion(): Promise<{ version: string; go: string; os: string; arch: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getStatus(): Promise<{ state: string; connectedAt?: string; error?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getConfig(): Promise<{ config?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async connect(_options: { config: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async disconnect(): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async checkWebUpdate(): Promise<WebUpdateInfo> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async checkNativeUpdate(): Promise<NativeUpdateInfo> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async applyWebUpdate(): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async openUrl(_options: { url: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async appendLogs(_options: { entries: Array<{ level: string; message: string; timestamp: number }> }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async uploadLogs(_options: { email?: string; reason: string; feedbackId?: string; platform?: string; version?: string }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async setLogLevel(_options: { level: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async setDevEnabled(_options: { enabled: boolean }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async debugDump(): Promise<Record<string, unknown>> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getUpdateChannel(): Promise<{ channel: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async setUpdateChannel(_options: { channel: string }): Promise<{ channel: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async storageGet(_options: { key: string }): Promise<{ value: string | null }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async storageSet(_options: { key: string; value: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async storageRemove(_options: { key: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async listInstalledApps(): Promise<{ apps: Array<{ packageName: string; label: string; iconUrl?: string }> }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async classifyApps(): Promise<{ classifications: Array<{ id: string; default: 'direct' | 'proxy'; hit_kind?: string; hit_pattern?: string }> }> {
    // Fail-soft (not throw): App Bypass on web degrades to all-proxy badges.
    return { classifications: [] };
  }

  async relayFetch(): Promise<{ response: string }> {
    // Web has no gomobile core — report relay unsupported (code:-1) so the
    // webapp transport learns to skip relay and use the direct fallback.
    return { response: JSON.stringify({ code: -1, message: 'relay unsupported on web' }) };
  }

  async relayAddNodes(): Promise<{ response: string }> {
    // No relay on web → node feed is a silent no-op (success envelope so callers
    // don't log an error for a platform that legitimately has no RelayManager).
    return { response: JSON.stringify({ code: 0, message: 'ok', data: { added: 0, total: 0 } }) };
  }

  async getDefaultGateway(): Promise<{ gateway: string | null }> {
    return { gateway: null };
  }

  async iapGetProducts(): Promise<{ products: [] }> {
    throw this.unavailable('IAP is not available on web');
  }

  async iapPurchase(): Promise<{ result: 'cancelled' }> {
    throw this.unavailable('IAP is not available on web');
  }

  async iapRestore(): Promise<{ transactions: [] }> {
    throw this.unavailable('IAP is not available on web');
  }

  async iapFinishTransaction(): Promise<void> {
    throw this.unavailable('IAP is not available on web');
  }
}
