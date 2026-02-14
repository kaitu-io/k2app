import { WebPlugin } from '@capacitor/core';

import type { K2PluginInterface } from './definitions';

export class K2PluginWeb extends WebPlugin implements K2PluginInterface {
  async checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getUDID(): Promise<{ udid: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getVersion(): Promise<{ version: string; go: string; os: string; arch: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getStatus(): Promise<{ state: string; connectedAt?: string; error?: string; wireUrl?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async getConfig(): Promise<{ wireUrl?: string }> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async connect(_options: { wireUrl: string }): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }

  async disconnect(): Promise<void> {
    throw this.unavailable('K2Plugin is not available on web');
  }
}
