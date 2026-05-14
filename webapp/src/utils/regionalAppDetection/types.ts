/**
 * Region-aware app-detection contract.
 *
 * The dispatcher in `./index.ts` picks an `AppDetector` based on the user's
 * `useConfigStore.country` (smart-routing's country selector). Non-matching
 * regions get the `noopDetector` and the UI's auto-detected section stays
 * empty.
 *
 * Reason categorisation is detector-internal — the public surface only
 * carries a fully-qualified i18n key the UI renders verbatim. Section/note
 * labels are detector-owned the same way, so adding a new region never
 * touches `AppBypass.tsx`.
 */
import type { InstalledApp } from '../../types/kaitu-core';

export interface AutoDetectedAppEntry {
  packageName: string;
  label: string;
  iconUrl?: string;
  /** Fully-qualified i18n key, e.g. 'dashboard:appBypass.cn.reasonInstaller'. */
  reasonKey: string;
}

export interface AppDetector {
  /** Stable region code (lowercase ISO alpha-2) or 'noop'. */
  readonly region: string;
  /** i18n key for the section header — interpolates {{count}}. */
  readonly sectionTitleKey: string;
  /** i18n key for the note shown when smart-routing is active. */
  readonly noteSmartKey: string;
  /** i18n key for the note shown when global mode is active (greyed list). */
  readonly noteGlobalKey: string;
  detect(apps: InstalledApp[]): AutoDetectedAppEntry[];
}
