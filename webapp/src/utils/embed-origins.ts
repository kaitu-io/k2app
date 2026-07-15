/**
 * Helpers for pages that embed the website in an iframe (Discover, Changelog)
 * and receive 'external-link' postMessages from it.
 */

/**
 * Origins accepted as senders of embed postMessages, derived from the iframe
 * URL (appConfig-driven baseURL) instead of hardcoded hosts — the site may
 * redirect between apex and www, so both siblings are accepted.
 */
export function allowedEmbedOrigins(embedUrl: string): Set<string> {
  const origins = new Set<string>();
  try {
    const u = new URL(embedUrl);
    origins.add(u.origin);
    const sibling = u.host.startsWith('www.') ? u.host.slice(4) : `www.${u.host}`;
    origins.add(`${u.protocol}//${sibling}`);
  } catch {
    // Invalid config URL — accept nothing rather than everything.
  }
  return origins;
}

/** Only http(s) URLs may be handed to _platform.openExternal. */
export function isSafeExternalUrl(url: string): boolean {
  try {
    const protocol = new URL(url).protocol;
    return protocol === 'http:' || protocol === 'https:';
  } catch {
    return false;
  }
}
