/**
 * Web Platform Utilities
 *
 * Provides platform-like utilities for pure web environments.
 * Used when webapp runs standalone without Tauri or Capacitor.
 *
 * Note: The webapp normally runs embedded in Tauri (desktop) or Capacitor (mobile),
 * where window._k2 is injected by the host platform. This module provides fallback
 * implementations for web-only scenarios.
 *
 * UDID generation has been removed from web-platform. In the split architecture,
 * UDID is provided by the daemon (standalone) or native layer (Tauri/Capacitor).
 */

import { webSecureStorage } from './secure-storage';

/**
 * Web Platform implementation
 *
 * Partial IPlatform implementation for web-only environments.
 * Does NOT include getUdid — UDID comes from daemon/native, not web.
 */
export const webPlatform = {
  os: 'web' as const,
  isDesktop: false,
  isMobile: false,
  version: '0.0.0',

  // Storage support
  storage: webSecureStorage,

  // Logging
  debug: (message: string) => console.debug('[Web]', message),
  warn: (message: string) => console.warn('[Web]', message),

  // Clipboard (using native Web APIs)
  writeClipboard: async (text: string) => {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    }
  },

  readClipboard: async (): Promise<string> => {
    if (navigator.clipboard) {
      return navigator.clipboard.readText();
    }
    return '';
  },

  // External links
  openExternal: async (url: string) => {
    window.open(url, '_blank', 'noopener,noreferrer');
  },
};
