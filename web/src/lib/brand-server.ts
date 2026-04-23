import 'server-only';
import { headers } from 'next/headers';
import { brandFromHost, brandFromHostOrLocale, type Brand } from './brands';

export async function getBrand(locale?: string): Promise<Brand> {
  const h = await headers();
  const host = h.get('host');
  if (locale) return brandFromHostOrLocale(host, locale);
  return brandFromHost(host);
}
