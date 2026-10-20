/**
 * First-party, cookieless conversion funnel — the ONLY place this site reports
 * behaviour. Events go to the Center's /api/px (the middleware adds X-K2-Brand,
 * and the Center treats this brand as cookieless: it neither reads nor sets a
 * cookie for the funnel and keeps no IP).
 *
 * Nothing is stored on the device: the visitor id is a random value held in this
 * module's memory, so it lives exactly as long as the loaded app — across
 * client-side navigation, not across a full reload or a new tab. Because no
 * information is stored in or read from the terminal equipment, PECR reg. 6
 * consent does not apply and there is no consent banner (see /privacy).
 * `Sec-GPC: 1` is honoured server-side (the event is counted with no visitor id).
 *
 * Reporting must never affect a product path: every failure is swallowed.
 */
export const FUNNEL_EVENTS = ['page_view', 'pricing_view', 'install_view', 'install_click', 'checkout_start', 'auth_done'] as const;
export type FunnelEvent = (typeof FUNNEL_EVENTS)[number];

let visitorId: string | null = null;

/** 16 random bytes, base64url without padding (22 chars) — the format the Center accepts. */
export function newVisitorId(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function visitor(): string {
  visitorId ??= newVisitorId();
  return visitorId;
}

/** Test hook. */
export function resetVisitorForTest(): void {
  visitorId = null;
}

/**
 * What may leave the browser as the page location: the path plus `utm_*` tags,
 * nothing else (query strings here carry sign-in and checkout state; the pixel URL
 * lands in access logs). The fragment is dropped too.
 */
export function pixelLocation(loc: string): string {
  const noHash = loc.split('#', 1)[0];
  const at = noHash.indexOf('?');
  if (at === -1) return noHash;
  const kept = new URLSearchParams();
  for (const [key, value] of new URLSearchParams(noHash.slice(at + 1))) {
    if (key.startsWith('utm_')) kept.append(key, value);
  }
  const query = kept.toString();
  return query ? `${noHash.slice(0, at)}?${query}` : noHash.slice(0, at);
}

export function pxUrl(event: FunnelEvent, opts: { plan?: string; ref?: string; loc?: string } = {}): string {
  const loc = pixelLocation(
    opts.loc ?? (typeof window !== 'undefined' ? window.location.pathname + window.location.search : ''),
  );
  const params = new URLSearchParams();
  params.set('e', event);
  params.set('a', visitor());
  if (loc) params.set('u', loc);
  if (opts.plan) params.set('p', opts.plan);
  if (opts.ref) params.set('r', opts.ref);
  return `/api/px?${params.toString()}`;
}

/**
 * Fire-and-forget. `keepalive` lets checkout_start survive the redirect to Stripe;
 * same-origin credentials let the Center tie auth_done to the signed-in account.
 */
export function track(event: FunnelEvent, opts?: { plan?: string; ref?: string }): void {
  if (typeof window === 'undefined') return;
  try {
    void fetch(pxUrl(event, opts), { keepalive: true, credentials: 'same-origin' }).catch(() => {});
  } catch {
    // analytics must never break a product path
  }
}

/** Host of an external referrer, if any (the landing page's traffic source). */
export function externalReferrerHost(): string | undefined {
  if (typeof document === 'undefined' || !document.referrer) return undefined;
  try {
    const host = new URL(document.referrer).host;
    return host && host !== window.location.host ? host : undefined;
  } catch {
    return undefined;
  }
}
