import type { InstalledApp } from '../types/kaitu-core';

export type RouteDefault = 'direct' | 'proxy';

interface ClassifyResult {
  id: string;
  default: RouteDefault;
  hit_kind?: string;
  hit_pattern?: string;
}

/**
 * Ask the daemon which installed apps default to direct vs proxy for `region`.
 * Uses the SAME krs.MatchInstalled codepath the engine runs at connect time,
 * so badges match routing. Fail-soft: empty region or any error → every app
 * defaults to 'proxy' (the safe default — traffic stays in the tunnel).
 */
export async function classifyApps(
  region: string,
  installed: InstalledApp[],
): Promise<Map<string, RouteDefault>> {
  const out = new Map<string, RouteDefault>();
  for (const a of installed) out.set(a.id, 'proxy');
  if (!region || installed.length === 0) return out;

  try {
    const resp = await window._k2.run<{ classifications: ClassifyResult[] }>(
      'classify-apps',
      {
        region,
        installed: installed.map((a) => ({
          id: a.id,
          label: a.label,
          installer_package_name: a.installerPackageName ?? '',
          process_names: a.processNames,
        })),
      },
    );
    if (resp.code === 0 && resp.data?.classifications) {
      for (const c of resp.data.classifications) {
        out.set(c.id, c.default === 'direct' ? 'direct' : 'proxy');
      }
    }
  } catch (e) {
    console.warn('[classifyApps] failed, defaulting all to proxy', e);
  }
  return out;
}
