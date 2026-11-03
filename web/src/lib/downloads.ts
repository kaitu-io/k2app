// web/src/lib/downloads.ts
import { siteBrand } from './brands';
import { getDownloadLinks, getAndroidDownloadLinks } from './constants';

export interface MobileLinks {
  ios: { url: string; version: string };
  android: { primary: string; backup: string; version: string };
}

export interface DesktopChannel {
  version: string;
  /**
   * Windows 可单独落后于 `version`：新 exe 过 SmartScreen 之前，manifest 的
   * `platforms.windows-x86_64` 仍指向上一版（scripts/CLAUDE.md「Windows 上线门」）。
   * `links.windows` 已按它生成。
   */
  windowsVersion: string;
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

/** `…/0.4.13/Kaitu_0.4.13_x64.exe` → `0.4.13`；认不出返回 null。 */
export function windowsVersionFromUrl(url: unknown): string | null {
  if (typeof url !== 'string') return null;
  const m = url.match(/\/([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?)\/[^/]+_x64\.exe$/);
  return m ? m[1] : null;
}

interface DesktopVersions {
  version: string;
  windowsVersion: string;
}

async function fetchDesktopVersion(channel: 'beta' | 'stable'): Promise<DesktopVersions | null> {
  const path = channel === 'beta' ? '/beta/cloudfront.latest.json' : '/cloudfront.latest.json';
  for (const base of siteBrand().cdn.desktopBases) {
    try {
      const res = await fetch(`${base}${path}`, { next: { revalidate: 300 } });
      if (res.ok) {
        const data = await res.json();
        if (data.version) {
          // 只有整份 manifest 没写 Windows 条目时才退回顶层版本（老格式）；
          // 写了却认不出 = 异常，同样退回，不让 Windows 按钮消失。
          const win = windowsVersionFromUrl(data.platforms?.['windows-x86_64']?.url);
          return { version: data.version, windowsVersion: win ?? data.version };
        }
      }
    } catch {}
  }
  return null;
}

function desktopChannel(v: DesktopVersions | null): DesktopChannel | null {
  if (!v) return null;
  const links = getDownloadLinks(v.version);
  links.windows = getDownloadLinks(v.windowsVersion).windows;
  return { version: v.version, windowsVersion: v.windowsVersion, links };
}

async function fetchMobileLinks(): Promise<MobileLinks | null> {
  for (const base of siteBrand().cdn.mobileBases) {
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
      beta: desktopChannel(betaVer),
      stable: desktopChannel(stableVer),
    },
    mobile,
  };
}
