// web/src/lib/downloads.ts
import { CDN_PRIMARY, CDN_BACKUP, getDownloadLinks, getAndroidDownloadLinks } from './constants';

const CDN_MOBILE_BASES = [
  'https://dl.kaitu.io/kaitu',
  'https://d13jc1jqzlg4yt.cloudfront.net/kaitu',
];

export interface MobileLinks {
  ios: { url: string; version: string };
  android: { primary: string; backup: string; version: string };
}

export interface DesktopChannel {
  version: string;
  links: ReturnType<typeof getDownloadLinks>;
}

export interface AllDownloadLinks {
  desktop: {
    beta: DesktopChannel | null;
    stable: DesktopChannel | null;
  };
  mobile: MobileLinks | null;
}

export function flattenToRecord(all: AllDownloadLinks): Record<string, string> {
  const ver = all.desktop.beta || all.desktop.stable;
  return {
    windows: ver?.links.windows.primary ?? '',
    macos: ver?.links.macos.primary ?? '',
    ios: all.mobile?.ios.url ?? '',
    android: all.mobile?.android.primary ?? '',
  };
}

async function fetchDesktopVersion(channel: 'beta' | 'stable'): Promise<string | null> {
  const path = channel === 'beta' ? '/beta/cloudfront.latest.json' : '/cloudfront.latest.json';
  for (const base of [CDN_PRIMARY, CDN_BACKUP]) {
    try {
      const res = await fetch(`${base}${path}`, { next: { revalidate: 300 } });
      if (res.ok) {
        const data = await res.json();
        if (data.version) return data.version;
      }
    } catch {}
  }
  return null;
}

async function fetchMobileLinks(): Promise<MobileLinks | null> {
  for (const base of CDN_MOBILE_BASES) {
    try {
      const [iosRes, androidRes] = await Promise.all([
        fetch(`${base}/ios/latest.json`, { next: { revalidate: 300 } }),
        fetch(`${base}/android/latest.json`, { next: { revalidate: 300 } }),
      ]);
      if (iosRes.ok && androidRes.ok) {
        const ios = await iosRes.json();
        const android = await androidRes.json();
        return {
          ios: { url: ios.appstore_url, version: ios.version },
          android: { ...getAndroidDownloadLinks(android.version), version: android.version },
        };
      }
    } catch {}
  }
  return null;
}

export async function fetchAllDownloadLinks(): Promise<AllDownloadLinks> {
  const [betaVer, stableVer, mobile] = await Promise.all([
    fetchDesktopVersion('beta'),
    fetchDesktopVersion('stable'),
    fetchMobileLinks(),
  ]);

  return {
    desktop: {
      beta: betaVer ? { version: betaVer, links: getDownloadLinks(betaVer) } : null,
      stable: stableVer ? { version: stableVer, links: getDownloadLinks(stableVer) } : null,
    },
    mobile,
  };
}
