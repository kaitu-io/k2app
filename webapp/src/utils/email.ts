const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * Permissive email shape check matching the regex used across the webapp
 * UI surface. NOT a substitute for server-side validation.
 *
 * Preserves the two-step semantics historically used inline:
 *   email.trim() !== '' && EMAIL_RE.test(email)
 * The regex runs against the ORIGINAL (untrimmed) string — only the
 * non-empty check uses `trim()`. Surrounding whitespace will therefore
 * still fail the regex, which is the intended behavior.
 */
export function isValidEmail(email: string): boolean {
  return email.trim() !== '' && EMAIL_RE.test(email);
}
