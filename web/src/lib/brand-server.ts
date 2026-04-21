import 'server-only';
import { headers } from 'next/headers';
import { brandFromHost, type Brand } from './brands';

export async function getBrand(): Promise<Brand> {
  const h = await headers();
  return brandFromHost(h.get('host'));
}
