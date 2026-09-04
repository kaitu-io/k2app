"use client";

export const dynamic = "force-dynamic";

import DelegateClient from "./DelegateClient";

/**
 * Delegate Payer Setup Page
 *
 * Allows users to set who pays for their subscription. Replaces the legacy
 * parent-led member model with a user-centric "set who pays for me" flow.
 * Supports ?returnTo=<path> to redirect after a successful save.
 */
export default function DelegatePage() {
  return <DelegateClient />;
}
