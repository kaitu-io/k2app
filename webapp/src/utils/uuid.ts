/**
 * crypto.randomUUID() with polyfill for older WebViews.
 *
 * Native crypto.randomUUID requires Chromium 92+ (2021). Older Huawei phones
 * are stuck on stale WebView (no Google Play auto-update post-2019), where
 * the API is missing and any direct call throws TypeError. Falls back to
 * crypto.getRandomValues (Chromium 11+) shaped into RFC 4122 v4.
 */
export function randomUUID(): string {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20, 32)}`;
}
