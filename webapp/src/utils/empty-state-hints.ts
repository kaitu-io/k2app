/**
 * Empty-state copy must only promise paths the CURRENT build actually has.
 *
 * The cloud-list empty states used to hard-code a single sentence that named
 * both escape hatches ("renew, or switch to Self-Deployed"). Both are gated:
 *  - renew → `purchaseSurfaceAvailable()` (utils/purchase-surface.ts); off on a
 *    Play-only brand and on iOS without the StoreKit bridge.
 *  - Self-Deployed → `brandConfig.features.selfHostedTunnels`; off on a brand
 *    with no k2s install channel, which also unregisters the /tunnels route.
 * With both off (Play-only brand on Android) the user got a dead end whose text
 * pointed at a tab that does not exist in that build.
 *
 * These pickers take the two gates as plain booleans so every branch is
 * testable without a brand or a platform shim; the caller passes the live
 * gates. Keys are returned (not rendered) so the component keeps owning `t`.
 */

/** i18n key for the "membership expired" empty state, by available escape hatch. */
export function membershipExpiredHintKey(canRenew: boolean, canSelfHost: boolean): string {
  if (canRenew && canSelfHost) return 'dashboard:dashboard.membershipExpiredHint';
  if (canRenew) return 'dashboard:dashboard.membershipExpiredHintRenewOnly';
  if (canSelfHost) return 'dashboard:dashboard.membershipExpiredHintSelfHostedOnly';
  // Neither path exists in this build. Say what is true and stop — a link out
  // to a web checkout would be the very payment steering that turned the
  // purchase surface off in the first place.
  return 'dashboard:dashboard.membershipExpiredHintNoAction';
}

/** i18n key for the "cloud nodes unavailable" (load failed) empty state. Retry
 *  is always offered by the button next to it; only the Self-Deployed clause
 *  is conditional. */
export function cloudNodesUnavailableHintKey(canSelfHost: boolean): string {
  return canSelfHost
    ? 'dashboard:dashboard.cloudNodesUnavailableHint'
    : 'dashboard:dashboard.cloudNodesUnavailableHintRetryOnly';
}
