import { brandConfig } from '../brands';

/**
 * Single authority for "may this build show any purchase / subscription-
 * management entry point". Every nav item, route registration and CTA that
 * leads to /purchase or opens a checkout/portal URL must consult this.
 *  - iOS without the StoreKit bridge: no (Apple 3.1.1 — no external purchase).
 *  - Android when the brand is Google-Play-only: no (Play Payments policy —
 *    a Play-distributed app must not steer users to a non-Play payment).
 * Desktop / browser / iOS-with-IAP / Android on a sideloaded brand: yes.
 */
export function purchaseSurfaceAvailable(): boolean {
  const os = window._platform?.os;
  if (os === 'ios' && !window._platform?.iap) return false;
  if (os === 'android' && !brandConfig.features.androidPurchase) return false;
  return true;
}
