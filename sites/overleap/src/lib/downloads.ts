import { DOWNLOADS } from './site';

/**
 * Download targets for the /install page: CDN manifests + store listings folded
 * into four platform cards. A missing artifact or an unpublished store listing is
 * url '' ("Coming soon") — never a null-versioned `Overleap_null_*` dead link.
 */

export type InstallPlatform = 'windows' | 'macos' | 'ios' | 'android';

export interface InstallTarget {
  platform: InstallPlatform;
  /** Download or store link; '' = not available yet. */
  url: string;
  /** Artifact version (desktop / Android APK). */
  version?: string;
  /** A store listing (App Store / Play) rather than a direct download. */
  store?: boolean;
}

export interface DesktopLinks {
  windows: string;
  macos: string;
}

export interface AllDownloadLinks {
  desktop: {
    beta: { version: string; links: DesktopLinks } | null;
    stable: { version: string; links: DesktopLinks } | null;
  };
  mobile: {
    ios: { url: string; version: string };
    android: { url: string; version: string };
  } | null;
}

export function desktopLinks(version: string): DesktopLinks {
  const base = DOWNLOADS.desktopBases[0];
  const p = DOWNLOADS.artifactPrefix;
  return {
    windows: `${base}/${version}/${p}_${version}_x64.exe`,
    macos: `${base}/${version}/${p}_${version}_universal.pkg`,
  };
}

export function androidApkLink(version: string): string {
  const p = DOWNLOADS.artifactPrefix;
  return `${DOWNLOADS.mobileBases[0]}/android/${version}/${p}-${version}.apk`;
}

async function fetchDesktopVersion(channel: 'beta' | 'stable'): Promise<string | null> {
  const path = channel === 'beta' ? '/beta/cloudfront.latest.json' : '/cloudfront.latest.json';
  for (const base of DOWNLOADS.desktopBases) {
    try {
      const res = await fetch(`${base}${path}`, { next: { revalidate: 300 } });
      if (res.ok) {
        const data = await res.json();
        if (data.version) return String(data.version);
      }
    } catch {
      /* try the next base */
    }
  }
  return null;
}

async function fetchMobileLinks(): Promise<AllDownloadLinks['mobile']> {
  for (const base of DOWNLOADS.mobileBases) {
    try {
      const [iosRes, androidRes] = await Promise.all([
        fetch(`${base}/ios/latest.json`, { next: { revalidate: 300 } }),
        fetch(`${base}/android/latest.json`, { next: { revalidate: 300 } }),
      ]);
      if (iosRes.ok && androidRes.ok) {
        const ios = await iosRes.json();
        const android = await androidRes.json();
        return {
          ios: { url: ios.appstore_url ?? '', version: ios.version ?? '' },
          android: { url: android.version ? androidApkLink(android.version) : '', version: android.version ?? '' },
        };
      }
    } catch {
      /* try the next base */
    }
  }
  return null;
}

export async function fetchAllDownloadLinks(): Promise<AllDownloadLinks> {
  const [beta, stable, mobile] = await Promise.all([
    fetchDesktopVersion('beta'),
    fetchDesktopVersion('stable'),
    fetchMobileLinks(),
  ]);
  return {
    desktop: {
      beta: beta ? { version: beta, links: desktopLinks(beta) } : null,
      stable: stable ? { version: stable, links: desktopLinks(stable) } : null,
    },
    mobile,
  };
}

/** iOS prefers the store listing (the manifest's appstore_url is a store link too); Android prefers Play, then the APK. */
export function buildInstallTargets(
  all: AllDownloadLinks,
  storeLinks: { ios: string; android: string } = DOWNLOADS.storeLinks,
): InstallTarget[] {
  const desktop = all.desktop.stable ?? all.desktop.beta;
  const ios = storeLinks.ios || all.mobile?.ios.url || '';
  const android = storeLinks.android || all.mobile?.android.url || '';
  return [
    { platform: 'windows', url: desktop?.links.windows ?? '', version: desktop?.version },
    { platform: 'macos', url: desktop?.links.macos ?? '', version: desktop?.version },
    { platform: 'ios', url: ios, store: true },
    { platform: 'android', url: android, store: Boolean(storeLinks.android), version: all.mobile?.android.version },
  ];
}
