/** Post-login destination: only same-site, locale-less paths ("/account"); anything else → /account.
 *  Rejects protocol-relative (//host) and backslash (/\\host) forms browsers treat as off-site. */
export function safeNext(raw: string | null): string {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) return '/account';
  return raw;
}
