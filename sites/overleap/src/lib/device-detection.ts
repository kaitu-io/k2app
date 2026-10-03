import type { InstallPlatform } from './downloads';

/** The visitor's platform from the user agent, or null (Linux, unknown, server). Highlight only — never auto-downloads. */
export function detectPlatform(userAgent?: string): InstallPlatform | null {
  const ua = (userAgent ?? (typeof navigator === 'undefined' ? '' : navigator.userAgent)).toLowerCase();
  if (!ua) return null;
  if (/iphone|ipad|ipod/.test(ua)) return 'ios';
  if (/android/.test(ua)) return 'android';
  if (/windows|win32|win64|wow32|wow64/.test(ua)) return 'windows';
  if (/macintosh|mac os x/.test(ua)) return 'macos';
  return null;
}
