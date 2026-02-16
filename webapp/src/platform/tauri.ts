import type { PlatformApi } from './types';

export class TauriPlatform implements PlatformApi {
  readonly isMobile = false;
  readonly platformName = 'tauri';

  async openExternal(url: string): Promise<void> {
    try {
      const shellModule = '@tauri-apps/plugin-shell';
      const { open } = await import(/* @vite-ignore */ shellModule);
      await open(url);
    } catch {
      // Native API not available (e.g. in tests)
    }
  }

  async writeClipboard(text: string): Promise<void> {
    await navigator.clipboard.writeText(text);
  }

  async syncLocale(locale: string): Promise<void> {
    try {
      const coreModule = '@tauri-apps/api/core';
      const { invoke } = await import(/* @vite-ignore */ coreModule);
      await invoke('sync_locale', { locale });
    } catch {
      // Native API not available (e.g. in tests)
    }
  }

  async uploadLogs(_feedbackId: string): Promise<void> {
    // No-op on desktop — logs are local
  }
}
