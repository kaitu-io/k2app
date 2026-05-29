/**
 * Pure mapping helpers for the Capacitor (Android) bridge.
 *
 * Kept in a separate module with no @capacitor/* imports so that
 * unit tests can import these functions without triggering Capacitor
 * side-effects or requiring the full vi.mock setup.
 */

import type { InstalledApp } from '../types/kaitu-core';

export interface AndroidInstalledApp {
  packageName: string;
  label: string;
  iconUrl?: string;
  installerPackageName?: string | null;
}

/**
 * Maps an Android PackageManager app record to the unified InstalledApp shape.
 *
 * packageName → id (stable platform id)
 * null/undefined installerPackageName → undefined (normalised)
 * processNames seeded with [packageName] (Android single-process default)
 */
export function mapInstalledApp(a: AndroidInstalledApp): InstalledApp {
  return {
    id: a.packageName,
    label: a.label,
    iconUrl: a.iconUrl,
    installerPackageName: a.installerPackageName ?? undefined,
    processNames: [a.packageName],
  };
}
