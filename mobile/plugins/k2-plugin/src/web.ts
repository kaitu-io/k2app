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
}
