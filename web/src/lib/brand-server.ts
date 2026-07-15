import 'server-only';
import { siteBrand, type Brand } from './brands';

/**
 * Phase 2: the brand is baked at build time (NEXT_PUBLIC_BRAND) — there is no
 * host/locale-based runtime resolution anymore. The async signature and the
 * (now ignored) locale parameter are kept so existing server call sites don't
 * churn; new code should import siteBrand() from './brands' directly.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- signature kept for call-site stability
export async function getBrand(_locale?: string): Promise<Brand> {
  return siteBrand();
}
