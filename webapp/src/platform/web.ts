import type { PlatformApi } from './types';

export class WebPlatform implements PlatformApi {
  readonly isMobile = false;
  readonly platformName = 'web';

  async openExternal(url: string): Promise<void> {
    window.open(url, '_blank');
  }

  async writeClipboard(text: string): Promise<void> {
    await navigator.clipboard.writeText(text);
  }

  async syncLocale(_locale: string): Promise<void> {
    // No-op on web
  }

  async uploadLogs(_feedbackId: string): Promise<void> {
    // No-op on web
  }
}
