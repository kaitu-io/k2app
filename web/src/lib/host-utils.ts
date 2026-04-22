const PRODUCTION_HOSTS = new Set([
  'kaitu.io',
  'www.kaitu.io',
  'overleap.io',
  'www.overleap.io',
]);

export function isProductionHost(host: string | null | undefined): boolean {
  if (!host) return false;
  const h = host.toLowerCase().split(':')[0];
  return PRODUCTION_HOSTS.has(h);
}
