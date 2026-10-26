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

/**
 * 掩码登录邮箱。星号数固定为 3，不随用户名长度增长——等长掩码会生成一个
 * 没有断行机会的长星号串，撑破手机窄屏下的行宽（且泄露用户名长度）。
 */
export function maskEmail(email: string): string {
  const at = email.lastIndexOf("@");
  if (at <= 0) return email;
  const username = email.slice(0, at);
  const domain = email.slice(at + 1);
  // 用户名不足 3 字符时只保留首字符，避免掩码后仍能还原
  const masked = username.length <= 2
    ? `${username.charAt(0)}***`
    : `${username.charAt(0)}***${username.charAt(username.length - 1)}`;
  return `${masked}@${domain}`;
}
