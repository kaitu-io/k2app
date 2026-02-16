export interface PlatformApi {
  openExternal(url: string): Promise<void>;
  writeClipboard(text: string): Promise<void>;
  syncLocale(locale: string): Promise<void>;
  uploadLogs(feedbackId: string): Promise<void>;
  readonly isMobile: boolean;
  readonly platformName: string;
}
