/**
 * Error Code Utilities
 * Maps error codes to user-friendly messages.
 *
 * The code→i18n table itself lives in `errorCatalog.ts` — this module is only
 * the rendering shell plus the one message-routed special case. See that file
 * for why there are two catalogs and what the coverage guard does.
 */

import { TFunction } from 'i18next';

import { API_ERROR_CATALOG, ERROR_CODES, UNKNOWN_ERROR_KEY } from './errorCatalog';
import { PASSWORD_MIN_LENGTH } from './password-strength';

// ERROR_CODES is declared next to the catalog it indexes; re-exported here so
// the ~30 existing `from './errorCode'` imports keep working.
export { ERROR_CODES };
export type { ErrorEntry } from './errorCatalog';

/**
 * Get error message by error code (API / Center-response domain).
 *
 * `message` is the raw backend `response.message`. It is NOT displayed to users
 * (that would violate webapp/CLAUDE.md "API Error Code Constitution"). It is
 * only used to disambiguate a small set of `ErrorInvalidArgument` (422)
 * sub-cases where the backend exposes a stable enum string — currently the
 * password strength validator (`password_too_short` / `password_too_weak`).
 *
 * @param code - Error code from response
 * @param message - Backend response.message (used only for enum routing)
 * @param t - i18next translation function
 * @param defaultMessage - Default message if no mapping found
 * @returns Localized error message
 */
export function getErrorMessage(
  code: number,
  message: string | undefined,
  t: TFunction,
  defaultMessage?: string
): string {
  // The ONLY message-based routing allowed by webapp/CLAUDE.md "API Error Code
  // Constitution": the backend's password strength validator returns a stable
  // enum string in `response.message`. See spec
  // `docs/superpowers/specs/2026-05-21-password-login-completion-design.md` §4.5.
  // Kept out of the catalog because it is a (code, message) pair, not a code.
  if (code === ERROR_CODES.INVALID_ARGUMENT) {
    if (message === 'password_too_short') return t('account:password.tooShort', { length: PASSWORD_MIN_LENGTH });
    if (message === 'password_too_weak') return t('account:password.tooWeak');
  }

  const entry = API_ERROR_CATALOG[code];
  if (!entry) {
    return defaultMessage || t(UNKNOWN_ERROR_KEY, 'Unknown error');
  }
  return t(entry.key, { defaultValue: entry.defaultValue });
}

/**
 * Check if response is successful
 * @param code - Error code from response
 * @returns true if success
 */
export function isSuccess(code: number): boolean {
  return code === ERROR_CODES.SUCCESS;
}

/**
 * Handle response error
 * Throws error with appropriate message based on code
 * @param code - Error code from response
 * @param message - Optional message from response
 * @param t - i18next translation function
 * @param defaultMessage - Default error message
 */
export function handleResponseError(
  code: number,
  message: string | undefined,
  t: TFunction,
  defaultMessage: string
): void {
  if (isSuccess(code)) {
    return;
  }

  // Prefer code-based message over response message. `message` is passed through
  // so getErrorMessage can route `ErrorInvalidArgument` sub-cases (e.g. password
  // strength enum) to specific i18n keys; the raw string is never shown to users.
  const errorMessage = getErrorMessage(code, message, t, defaultMessage);
  throw new Error(errorMessage);
}
