/**
 * Conversion-funnel pixel — the ONLY place the website reports behaviour events.
 *
 * Event names are locked to contracts/api-contract.json (`funnelEvents` whose surfaces
 * include "web") by tests/funnel-contract.test.ts. Pages only build a URL; all
 * validation, consent (GPC / opt-out) and storage happen on the server behind /api/px.
 * Reporting must never affect a product path: every failure is swallowed.
 */
export const WEB_FUNNEL_EVENTS = [
  'page_view', 'pricing_view', 'checkout_view', 'install_view', 'welcome_view',
  'install_click', 'checkout_cancelled', 'refund_click', 'cancel_click',
  'plan_select', 'auth_code_sent', 'auth_done', 'checkout_start',
] as const;

export type WebFunnelEvent = (typeof WEB_FUNNEL_EVENTS)[number];

export interface PixelOpts {
  plan?: string;
  source?: string;
  ref?: string;
  loc?: string;
}

/**
 * What may leave the browser as the page location: the path plus `utm_*`
 * campaign tags, nothing else. Query strings on this site carry sign-in tokens,
 * payment session ids and invite codes; the pixel URL lands in access logs, so
 * every other parameter (and the fragment) is dropped here, before the request
 * exists, rather than trusted to the server to discard.
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

export function pxUrl(event: WebFunnelEvent, opts: PixelOpts = {}): string {
  const loc = pixelLocation(
    opts.loc ?? (typeof window !== 'undefined' ? window.location.pathname + window.location.search : ''),
  );
  const params = new URLSearchParams();
  params.set('e', event);
  if (loc) params.set('u', loc);
  if (opts.plan) params.set('p', opts.plan);
  if (opts.source) params.set('s', opts.source);
  if (opts.ref) params.set('r', opts.ref);
  return `/api/px?${params.toString()}`;
}

export function track(event: WebFunnelEvent, opts?: { plan?: string; source?: string; ref?: string }): void {
  if (typeof window === 'undefined') return;
  try {
    new Image().src = pxUrl(event, opts);
  } catch {
    // analytics must never break a product path
  }
}

/**
 * The apps load site pages in an iframe with `?embed=true` (or `#embed`) — the
 * same test `useEmbedMode` applies. Those are app screens, not site visits.
 */
export function isEmbeddedPage(): boolean {
  if (typeof window === 'undefined') return false;
  return new URLSearchParams(window.location.search).get('embed') === 'true' || window.location.hash === '#embed';
}

export function externalReferrerHost(): string | undefined {
  if (typeof document === 'undefined' || !document.referrer) return undefined;
  try {
    const host = new URL(document.referrer).host;
    return host && host !== window.location.host ? host : undefined;
  } catch {
    return undefined;
  }
}
