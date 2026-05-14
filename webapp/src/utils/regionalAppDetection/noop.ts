import type { AppDetector } from './types';

/**
 * No-op detector returned by the dispatcher when the user's country has no
 * registered detector. Used for every non-CN country today, including the
 * `country === null` initial state. UI never renders the auto-detected
 * section because `detect()` returns an empty list — the label keys are
 * placeholders that won't be looked up.
 */
export const noopDetector: AppDetector = {
  region: 'noop',
  sectionTitleKey: '',
  noteSmartKey: '',
  noteGlobalKey: '',
  detect: () => [],
};
