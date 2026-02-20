// Authentication utilities for the web application

// Supported locales - must match i18n/routing.ts
const LOCALES = ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'];

/**
 * Remove locale prefix from path
 * Example: /zh-CN/manager/orders -> /manager/orders
 */
function removeLocalePrefix(path: string): string {
  const segments = path.split('/');
  if (segments.length >= 2 && LOCALES.includes(segments[1])) {
    // Remove locale segment (first segment is empty string from leading /)
    segments.splice(1, 1);
    return segments.join('/') || '/';
  }
  return path;
}

/**
 * Simple redirect to login page with optional next parameter
 * Note: The next parameter should NOT include locale prefix
 * because next-intl router will add it automatically
 */
export function redirectToLogin(currentPath?: string): void {
  if (typeof window === 'undefined') return;

  const pathToUse = currentPath || (window.location.pathname + window.location.search);
  // Remove locale prefix from path to avoid double locale in redirect
  const pathWithoutLocale = removeLocalePrefix(pathToUse);
  const loginUrl = `/login?next=${encodeURIComponent(pathWithoutLocale)}`;
  window.location.href = loginUrl;
} 