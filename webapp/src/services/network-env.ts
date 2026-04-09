export interface NetworkEnvData {
  publicIP: string;
  isp: string;
  city: string;
  country: string;
  networkType: string;
}

let cached: NetworkEnvData | null = null;

/**
 * Probe ipinfo.io for public IP + ISP + geo. Best-effort, 3s timeout.
 * Caches the result — call refreshNetworkEnv() to re-probe.
 */
export async function getNetworkEnv(): Promise<NetworkEnvData> {
  if (cached) return cached;
  return refreshNetworkEnv();
}

export async function refreshNetworkEnv(): Promise<NetworkEnvData> {
  const empty: NetworkEnvData = { publicIP: '', isp: '', city: '', country: '', networkType: '' };
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 3000);
    const resp = await fetch('https://ipinfo.io/json', { signal: controller.signal });
    clearTimeout(timeout);

    if (!resp.ok) {
      cached = empty;
      return empty;
    }

    const data = await resp.json();
    cached = {
      publicIP: data.ip || '',
      isp: data.org || '',
      city: data.city || '',
      country: data.country || '',
      networkType: getNetworkType(),
    };
    return cached;
  } catch {
    cached = empty;
    return empty;
  }
}

function getNetworkType(): string {
  const conn = (navigator as any).connection;
  if (!conn) return '';
  return conn.type || conn.effectiveType || '';
}

// Re-probe on network change (online event or Connection API change).
// Guarded to avoid duplicate listeners on HMR.
const LISTENER_KEY = '__k2_network_env_listener__';
if (typeof window !== 'undefined' && !(window as any)[LISTENER_KEY]) {
  (window as any)[LISTENER_KEY] = true;
  window.addEventListener('online', () => {
    refreshNetworkEnv().catch(() => {});
  });
  const conn = (navigator as any).connection;
  if (conn?.addEventListener) {
    conn.addEventListener('change', () => {
      refreshNetworkEnv().catch(() => {});
    });
  }
}
