import 'server-only';
import { headers } from 'next/headers';

/**
 * Read the request pathname (stripped of locale prefix) from the `x-pathname`
 * request header injected by `src/middleware.ts`. Falls back to `''` when
 * absent (e.g. static generation time where request context isn't available).
 *
 * Returns values like `'/install'`, `'/purchase'`, `'/k2/comparison'`.
 * Returns `''` for the homepage (incoming `'/'`), so downstream URL
 * construction (`{base}/{locale}{pathname}`) doesn't produce a trailing slash.
 * Never returns a locale prefix like `'/zh-CN'` — the middleware strips it
 * before injection.
 */
export async function getRequestPathname(): Promise<string> {
  const h = await headers();
  const raw = h.get('x-pathname') ?? '';
  // Normalize: '/' → '' so downstream URL construction doesn't produce trailing slash
  return raw === '/' ? '' : raw;
}
