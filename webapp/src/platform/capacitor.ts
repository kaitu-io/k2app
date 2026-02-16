import type { PlatformApi } from './types';

export class CapacitorPlatform implements PlatformApi {
  readonly isMobile = true;
  readonly platformName = 'capacitor';

  async openExternal(url: string): Promise<void> {
    try {
      const browserModule = '@capacitor/browser';
      const { Browser } = await import(/* @vite-ignore */ browserModule);
      await Browser.open({ url });
    } catch {
      // Native API not available (e.g. in tests)
    }
  }

  async writeClipboard(text: string): Promise<void> {
    await navigator.clipboard.writeText(text);
  }

  async syncLocale(locale: string): Promise<void> {
    try {
      const pluginModule = 'k2-plugin';
      const { K2Plugin } = await import(/* @vite-ignore */ pluginModule);
      await K2Plugin.syncLocale({ locale });
    } catch {
      // Native API not available (e.g. in tests)
    }
  }

  async uploadLogs(feedbackId: string): Promise<void> {
    try {
      const pluginModule = 'k2-plugin';
      const { K2Plugin } = await import(/* @vite-ignore */ pluginModule);
      await K2Plugin.uploadLogs({ feedbackId });
    } catch {
      // Native API not available (e.g. in tests)
    }
  }
}
