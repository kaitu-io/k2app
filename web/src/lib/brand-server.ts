import 'server-only';
import { siteBrand, type Brand } from './brands';

/**
 * The site serves a single brand (siteBrand()). The async signature and the
 * ignored locale parameter are kept so existing server call sites don't churn;
 * new code should import siteBrand() from './brands' directly.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- signature kept for call-site stability
export async function getBrand(_locale?: string): Promise<Brand> {
  return siteBrand();
}
