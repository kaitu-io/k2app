import type { PlatformApi } from './types';

export class TauriPlatform implements PlatformApi {
  readonly isMobile = false;
  readonly platformName = 'tauri';

  openExternal(_url: string): Promise<void> {
    throw new Error('Not implemented');
  }

  writeClipboard(_text: string): Promise<void> {
    throw new Error('Not implemented');
  }

  syncLocale(_locale: string): Promise<void> {
    throw new Error('Not implemented');
  }

  uploadLogs(_feedbackId: string): Promise<void> {
    throw new Error('Not implemented');
  }
}
