/**
 * Funnel event vocabulary reported by the app surface.
 * Mirrored by the Center API funnel registry; cross-layer-contract.test.ts
 * asserts the two sets are equal.
 */
export const APP_FUNNEL_EVENTS = [
  'app_first_open',
  'first_connect_attempt',
  'first_connect_ok',
  'connect_ok',
  'manage_click',
  'login_view',
  'paywall_view',
  'plan_select',
  'auth_code_sent',
  'auth_done',
  'checkout_start',
] as const;
export type AppFunnelEvent = typeof APP_FUNNEL_EVENTS[number];

export const PAYWALL_SOURCES = [
  'tunnel_locked',
  'membership_guard',
  'login_dialog',
  'account',
  'account_expired',
  'direct',
] as const;
export type PaywallSource = typeof PAYWALL_SOURCES[number];

export interface FunnelProps {
  plan?: string;
  source?: string;
  channel?: 'stripe' | 'apple';
}
