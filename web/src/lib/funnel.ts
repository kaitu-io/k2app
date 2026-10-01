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

export function pxUrl(event: WebFunnelEvent, opts: PixelOpts = {}): string {
  const loc = opts.loc ?? (typeof window !== 'undefined' ? window.location.pathname + window.location.search : '');
  const params = new URLSearchParams();
  params.set('e', event);
  if (loc) params.set('u', loc);
  if (opts.plan) params.set('p', opts.plan);
  if (opts.source) params.set('s', opts.source);
  if (opts.ref) params.set('r', opts.ref);
  return `/api/px?${params.toString()}`;
}

export function track(event: WebFunnelEvent, opts?: { plan?: string; source?: string }): void {
  if (typeof window === 'undefined') return;
  try {
    new Image().src = pxUrl(event, opts);
  } catch {
    // analytics must never break a product path
  }
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
