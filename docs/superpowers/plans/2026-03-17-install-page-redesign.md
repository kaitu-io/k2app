# Install Page Redesign Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign `/install` page with Smart Hero + 3-column grid + FAQ accordion, and eliminate `DOWNLOAD_LINKS` in favor of CDN latest.json as single source of truth.

**Architecture:** Extract shared `lib/downloads.ts` for ISR-based CDN manifest fetching. Install page and invite page both consume this. InstallClient rewritten with device-aware Hero, 3-col desktop grid, inline mobile links, and FAQ accordion. All Waymaker branding removed.

**Tech Stack:** Next.js 15 (App Router, ISR), React 19, TypeScript, Tailwind CSS 4, shadcn/ui, next-intl, vitest

**Spec:** `docs/superpowers/specs/2026-03-17-install-page-redesign.md`

---

### Task 1: Create `lib/downloads.ts` with tests

The foundation — shared fetch logic for all download links from CDN manifests.

**Files:**
- Create: `web/src/lib/downloads.ts`
- Create: `web/tests/downloads.test.ts`
- Read: `web/src/lib/constants.ts` (for CDN_PRIMARY, CDN_BACKUP, getDownloadLinks)

- [ ] **Step 1: Write failing tests for `fetchAllDownloadLinks` and `flattenToRecord`**

```typescript
// web/tests/downloads.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock fetch globally
const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

// Mock constants
vi.mock('@/lib/constants', () => ({
  CDN_PRIMARY: 'https://cdn-primary.test/kaitu/desktop',
  CDN_BACKUP: 'https://cdn-backup.test/kaitu/desktop',
  getDownloadLinks: (version: string) => ({
    windows: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_x64.exe`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_x64.exe`,
    },
    macos: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_universal.pkg`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_universal.pkg`,
    },
    linux: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_amd64.AppImage`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_amd64.AppImage`,
    },
  }),
}));

describe('fetchAllDownloadLinks', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('fetches desktop beta + stable versions and mobile links', async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/beta/cloudfront.latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0-beta.1' }) });
      }
      if (url.includes('/cloudfront.latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.3.22' }) });
      }
      if (url.includes('/ios/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ appstore_url: 'https://apps.apple.com/app/id6448744655' }) });
      }
      if (url.includes('/android/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ url: 'https://d0.all7.cc/kaitu/android/0.4.0/Kaitu-0.4.0.apk' }) });
      }
      return Promise.resolve({ ok: false });
    });

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).not.toBeNull();
    expect(result.desktop.beta!.version).toBe('0.4.0-beta.1');
    expect(result.desktop.stable).not.toBeNull();
    expect(result.desktop.stable!.version).toBe('0.3.22');
    expect(result.mobile).not.toBeNull();
    expect(result.mobile!.ios).toBe('https://apps.apple.com/app/id6448744655');
    expect(result.mobile!.android).toBe('https://d0.all7.cc/kaitu/android/0.4.0/Kaitu-0.4.0.apk');
  });

  it('returns null for desktop channels when CDN is unreachable', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'));

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).toBeNull();
    expect(result.desktop.stable).toBeNull();
    expect(result.mobile).toBeNull();
  });

  it('falls back to backup CDN when primary fails', async () => {
    mockFetch.mockImplementation((url: string) => {
      // Primary fails for desktop
      if (url.startsWith('https://cdn-primary.test') && url.includes('cloudfront.latest.json')) {
        return Promise.resolve({ ok: false });
      }
      // Backup succeeds for desktop
      if (url.startsWith('https://cdn-backup.test') && url.includes('cloudfront.latest.json')) {
        if (url.includes('/beta/')) return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0-beta.1' }) });
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.3.22' }) });
      }
      // Primary fails for mobile
      if (url.startsWith('https://dl.kaitu.io')) {
        return Promise.resolve({ ok: false });
      }
      // Backup succeeds for mobile
      if (url.includes('/ios/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ appstore_url: 'https://apps.apple.com/app/test' }) });
      }
      if (url.includes('/android/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ url: 'https://cdn/android.apk' }) });
      }
      return Promise.resolve({ ok: false });
    });

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).not.toBeNull();
    expect(result.desktop.stable).not.toBeNull();
  });
});

describe('flattenToRecord', () => {
  it('flattens AllDownloadLinks to Record<string, string> for device-detection compat', async () => {
    const { flattenToRecord } = await import('../src/lib/downloads');

    const result = flattenToRecord({
      desktop: {
        beta: {
          version: '0.4.0-beta.1',
          links: {
            windows: { primary: 'https://cdn/win.exe', backup: 'https://backup/win.exe' },
            macos: { primary: 'https://cdn/mac.pkg', backup: 'https://backup/mac.pkg' },
            linux: { primary: 'https://cdn/linux.AppImage', backup: 'https://backup/linux.AppImage' },
          },
        },
        stable: null,
      },
      mobile: {
        ios: 'https://apps.apple.com/app/id123',
        android: 'https://cdn/android.apk',
      },
    });

    expect(result).toEqual({
      windows: 'https://cdn/win.exe',
      macos: 'https://cdn/mac.pkg',
      ios: 'https://apps.apple.com/app/id123',
      android: 'https://cdn/android.apk',
    });
  });

  it('returns empty strings when no data available', async () => {
    const { flattenToRecord } = await import('../src/lib/downloads');

    const result = flattenToRecord({
      desktop: { beta: null, stable: null },
      mobile: null,
    });

    expect(result).toEqual({ windows: '', macos: '', ios: '', android: '' });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run tests/downloads.test.ts`
Expected: FAIL — `../src/lib/downloads` does not exist

- [ ] **Step 3: Implement `lib/downloads.ts`**

```typescript
// web/src/lib/downloads.ts
import { CDN_PRIMARY, CDN_BACKUP, getDownloadLinks } from './constants';

const CDN_MOBILE_BASES = [
  'https://dl.kaitu.io/kaitu',
  'https://d13jc1jqzlg4yt.cloudfront.net/kaitu',
];

export interface MobileLinks {
  ios: string;
  android: string;
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
    ios: all.mobile?.ios ?? '',
    android: all.mobile?.android ?? '',
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
          ios: ios.appstore_url,
          android: android.url,
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run tests/downloads.test.ts`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd web && git add src/lib/downloads.ts tests/downloads.test.ts
git commit -m "feat(install): add lib/downloads.ts — CDN manifest fetch as single source of truth"
```

---

### Task 2: Delete `DOWNLOAD_LINKS` and clean up `constants.ts` + `next.config.ts`

Remove the old build-time download link constants. Update test mocks accordingly.

**Files:**
- Modify: `web/src/lib/constants.ts` (delete DOWNLOAD_LINKS, BETA_VERSION, DESKTOP_VERSION)
- Modify: `web/next.config.ts` (delete getRootPackageJson, NEXT_PUBLIC env vars)
- Modify: `web/tests/complex-pages-ssr.test.ts` (update mocks)

- [ ] **Step 1: Update test mocks to remove DOWNLOAD_LINKS dependency**

In `web/tests/complex-pages-ssr.test.ts`, replace the `@/lib/constants` mock (around line 50-58):

```typescript
// Replace with:
vi.mock('@/lib/constants', () => ({
  CDN_PRIMARY: 'https://cdn-primary.test/kaitu/desktop',
  CDN_BACKUP: 'https://cdn-backup.test/kaitu/desktop',
  getDownloadLinks: (version: string) => ({
    windows: { primary: `https://cdn/${version}/win.exe`, backup: `https://backup/${version}/win.exe` },
    macos: { primary: `https://cdn/${version}/mac.pkg`, backup: `https://backup/${version}/mac.pkg` },
    linux: { primary: `https://cdn/${version}/linux.AppImage`, backup: `https://backup/${version}/linux.AppImage` },
  }),
}));
```

Also add a mock for `@/lib/downloads` (after the constants mock):

```typescript
vi.mock('@/lib/downloads', () => ({
  fetchAllDownloadLinks: vi.fn().mockResolvedValue({
    desktop: {
      beta: { version: '0.4.0-beta.1', links: {
        windows: { primary: 'https://cdn/win.exe', backup: '' },
        macos: { primary: 'https://cdn/mac.pkg', backup: '' },
        linux: { primary: 'https://cdn/linux.AppImage', backup: '' },
      }},
      stable: { version: '0.3.22', links: {
        windows: { primary: 'https://cdn/win.exe', backup: '' },
        macos: { primary: 'https://cdn/mac.pkg', backup: '' },
        linux: { primary: 'https://cdn/linux.AppImage', backup: '' },
      }},
    },
    mobile: { ios: 'https://apps.apple.com/app/id123', android: 'https://cdn/android.apk' },
  }),
  flattenToRecord: vi.fn().mockReturnValue({
    windows: 'https://cdn/win.exe',
    macos: 'https://cdn/mac.pkg',
    ios: 'https://apps.apple.com/app/id123',
    android: 'https://cdn/android.apk',
  }),
}));
```

- [ ] **Step 2: Run existing tests to confirm mocks compile**

Run: `cd web && npx vitest run tests/complex-pages-ssr.test.ts`
Expected: PASS

- [ ] **Step 3: Delete `DOWNLOAD_LINKS`, `BETA_VERSION`, `DESKTOP_VERSION` from `constants.ts`**

Delete lines 4-6 (DESKTOP_VERSION, BETA_VERSION) and lines 27-34 (DOWNLOAD_LINKS object). Keep `CDN_PRIMARY`, `CDN_BACKUP`, `getDownloadLinks`, and all other constants.

- [ ] **Step 4: Clean up `next.config.ts`**

Delete the `getRootPackageJson` function (lines 19-28), the `rootPkg`/`desktopVersion`/`betaVersion` variables and console.log (lines 30-33), and the `NEXT_PUBLIC_DESKTOP_VERSION`/`NEXT_PUBLIC_BETA_VERSION` entries from the `env` block (lines 37-39). If `env` block becomes empty, remove it entirely.

- [ ] **Step 5: Run full test suite**

Run: `cd web && npx vitest run`
Expected: PASS. If any test still imports `DOWNLOAD_LINKS` or `DESKTOP_VERSION`, fix those imports.

- [ ] **Step 6: Commit**

```bash
cd web && git add src/lib/constants.ts next.config.ts tests/complex-pages-ssr.test.ts
git commit -m "refactor(install): delete DOWNLOAD_LINKS — CDN latest.json is single source of truth"
```

---

### Task 3: Rewire install `page.tsx` to use `fetchAllDownloadLinks`

**Files:**
- Modify: `web/src/app/[locale]/install/page.tsx`

- [ ] **Step 1: Run existing install page tests as baseline**

Run: `cd web && npx vitest run tests/complex-pages-ssr.test.ts`
Expected: PASS

- [ ] **Step 2: Rewrite `install/page.tsx` server component**

Replace the local `fetchDesktopVersion` with `fetchAllDownloadLinks` from `lib/downloads`. Pass `mobileLinks` as a new prop to `InstallClient`. Remove the local `fetchDesktopVersion` function and `CDN_PRIMARY`/`CDN_BACKUP` imports from constants (now handled by downloads.ts).

Key changes:
- Import `fetchAllDownloadLinks` from `@/lib/downloads`
- Delete local `fetchDesktopVersion` function
- Call `const all = await fetchAllDownloadLinks()`
- Extract `betaVersion`, `stableVersion` from `all.desktop`
- Pass `mobileLinks={all.mobile}` to `<InstallClient>`
- Move h1/subtitle into InstallClient (Hero owns all user-facing content)
- Keep JSON-LD script with `stableVersion || betaVersion`

See spec Section 4.3 for exact code.

- [ ] **Step 3: Run install page tests**

Run: `cd web && npx vitest run tests/complex-pages-ssr.test.ts`
Expected: PASS (InstallClient is mocked out in these tests)

- [ ] **Step 4: Commit**

```bash
cd web && git add src/app/\\[locale\\]/install/page.tsx
git commit -m "refactor(install): use fetchAllDownloadLinks in install page server component"
```

---

### Task 4: Rewire invite `page.tsx` to use `fetchAllDownloadLinks`

**Files:**
- Modify: `web/src/app/[locale]/s/[code]/page.tsx`
- Modify: `web/src/app/[locale]/s/[code]/InviteClient.tsx`

- [ ] **Step 1: Run existing invite page tests as baseline**

Run: `cd web && npx vitest run tests/complex-pages-ssr.test.ts`
Expected: PASS

- [ ] **Step 2: Update `s/[code]/page.tsx`**

- Delete `export const dynamic = 'force-static'`
- Add `export const revalidate = 300`
- Import `fetchAllDownloadLinks`, `flattenToRecord` from `@/lib/downloads`
- Call `const all = await fetchAllDownloadLinks()` in the server component
- Pass `downloadLinks={flattenToRecord(all)}` to `<InviteClient>`

- [ ] **Step 3: Update `InviteClient.tsx`**

- Delete `import { DOWNLOAD_LINKS } from '@/lib/constants'`
- Add `downloadLinks: Record<string, string>` to component props
- Replace all `DOWNLOAD_LINKS` references with `downloadLinks` prop:
  - Line 78: `getPrimaryDownloadLink(DOWNLOAD_LINKS)` → `getPrimaryDownloadLink(downloadLinks)`
  - Lines 304-368: `DOWNLOAD_LINKS.windows`, `.macos`, `.ios`, `.android` → `downloadLinks.windows`, etc.

- [ ] **Step 4: Run invite page tests**

Run: `cd web && npx vitest run tests/complex-pages-ssr.test.ts`
Expected: PASS

- [ ] **Step 5: Verify zero remaining `DOWNLOAD_LINKS` references**

Run: `grep -r "DOWNLOAD_LINKS" web/src/`
Expected: No output (zero references)

- [ ] **Step 6: Commit**

```bash
cd web && git add src/app/\\[locale\\]/s/\\[code\\]/page.tsx src/app/\\[locale\\]/s/\\[code\\]/InviteClient.tsx
git commit -m "refactor(invite): use fetchAllDownloadLinks, replace DOWNLOAD_LINKS with server props"
```

---

### Task 5: Add shadcn accordion component

**Files:**
- Create: `web/src/components/ui/accordion.tsx`

- [ ] **Step 1: Install accordion component**

Run: `cd web && npx shadcn@latest add accordion`
Expected: Creates `src/components/ui/accordion.tsx`

- [ ] **Step 2: Verify file exists**

Run: `ls web/src/components/ui/accordion.tsx`
Expected: File exists

- [ ] **Step 3: Commit**

```bash
cd web && git add src/components/ui/accordion.tsx package.json yarn.lock
git commit -m "feat(ui): add shadcn accordion component"
```

---

### Task 6: Update i18n keys (all 7 locales)

Delete Waymaker-related keys, add Hero/install-steps/FAQ keys.

**Files:**
- Modify: `web/messages/zh-CN/install.json`
- Modify: `web/messages/en-US/install.json`
- Modify: `web/messages/en-GB/install.json`
- Modify: `web/messages/en-AU/install.json`
- Modify: `web/messages/zh-TW/install.json`
- Modify: `web/messages/zh-HK/install.json`
- Modify: `web/messages/ja/install.json`

- [ ] **Step 1: Update zh-CN/install.json (primary locale)**

Delete keys: `mobileComingSoon`, `platformDevelopment`, `useDesktopVersion`, `waymakerApp`, `waymakerNote`, `kaituMobileComingSoon`, `appStore`, `downloadApk`, `comingSoon`.

Add keys under `install`:

```json
"otherPlatforms": "其他平台",
"terminalInstall": "或通过终端一键安装",
"downloadButton": "下载安装包",
"mobileDownloads": "移动端",
"kaituIos": "开途 iOS 版",
"kaituAndroid": "开途安卓版",
"heroTitle": {
  "windows": "为你的 Windows 准备好了",
  "macos": "为你的 Mac 准备好了",
  "linux": "为你的 Linux 准备好了",
  "ios": "开途 iOS 版",
  "android": "开途安卓版",
  "unknown": "选择你的平台"
},
"installSteps": {
  "title": "安装步骤",
  "windows": {
    "1": "打开下载的 .exe 安装文件",
    "2": "如果提示"Windows 已保护你的电脑"，点击"更多信息" → "仍要运行"",
    "3": "按提示完成安装，首次启动需要管理员权限"
  },
  "macos": {
    "1": "打开下载的 .pkg 安装文件",
    "2": "按提示完成安装",
    "3": "首次打开时，在"系统设置 → 隐私与安全性"中允许系统扩展"
  },
  "linux": {
    "1": "给 AppImage 添加执行权限：chmod +x Kaitu_*.AppImage",
    "2": "双击运行或在终端执行 ./Kaitu_*.AppImage",
    "3": "推荐使用命令行安装以自动处理依赖"
  },
  "android": {
    "1": "在"设置 → 安全"中允许安装未知来源应用",
    "2": "打开下载的 APK 文件完成安装"
  }
},
"faq": {
  "browserBlock": {
    "question": "浏览器阻止了下载怎么办？",
    "answer": "部分浏览器会拦截 .exe 或 .pkg 文件的下载。请在浏览器下载栏中选择"保留"或"仍要下载"。如果使用 Chrome，可以在下载页面（chrome://downloads）找到被拦截的文件。"
  },
  "windowsSmartScreen": {
    "question": "Windows 提示"已保护你的电脑"怎么办？",
    "answer": "这是 Windows SmartScreen 对新应用的默认提示。点击"更多信息"，然后点击"仍要运行"即可。开途客户端已通过 EV 代码签名，安装后不会再出现此提示。"
  },
  "macosGatekeeper": {
    "question": "macOS 提示"无法验证开发者"怎么办？",
    "answer": "首次安装后，打开"系统设置 → 隐私与安全性"，在底部找到关于开途的提示，点击"仍要打开"。后续启动不会再提示。如需启用系统扩展（VPN 模式），系统会引导你到"隐私与安全性 → 通用"中允许。"
  },
  "security": {
    "question": "开途客户端安全吗？",
    "answer": "开途客户端已通过 Apple 公证和 Windows EV 代码签名。所有安装包通过 HTTPS 分发，可验证 SHA-256 校验和。k2 协议核心代码计划开源。"
  }
}
```

- [ ] **Step 2: Update en-US/install.json**

Same structure, English translations:

```json
"otherPlatforms": "Other Platforms",
"terminalInstall": "Or install via terminal",
"downloadButton": "Download Installer",
"mobileDownloads": "Mobile",
"kaituIos": "Kaitu for iOS",
"kaituAndroid": "Kaitu for Android",
"heroTitle": {
  "windows": "Ready for your Windows",
  "macos": "Ready for your Mac",
  "linux": "Ready for your Linux",
  "ios": "Kaitu for iOS",
  "android": "Kaitu for Android",
  "unknown": "Choose your platform"
},
"installSteps": {
  "title": "Installation Steps",
  "windows": {
    "1": "Open the downloaded .exe installer",
    "2": "If 'Windows protected your PC' appears, click 'More info' then 'Run anyway'",
    "3": "Follow the prompts to complete installation. Admin privileges required on first launch."
  },
  "macos": {
    "1": "Open the downloaded .pkg installer",
    "2": "Follow the prompts to complete installation",
    "3": "On first launch, allow the system extension in System Settings then Privacy and Security"
  },
  "linux": {
    "1": "Make the AppImage executable: chmod +x Kaitu_*.AppImage",
    "2": "Double-click to run or execute ./Kaitu_*.AppImage in terminal",
    "3": "CLI install recommended for automatic dependency handling"
  },
  "android": {
    "1": "Allow installing from unknown sources in Settings then Security",
    "2": "Open the downloaded APK to install"
  }
},
"faq": {
  "browserBlock": {
    "question": "Browser blocked the download?",
    "answer": "Some browsers block .exe or .pkg downloads by default. Click 'Keep' or 'Download anyway' in the browser download bar. In Chrome, visit chrome://downloads to find blocked files."
  },
  "windowsSmartScreen": {
    "question": "Windows says 'Windows protected your PC'?",
    "answer": "This is Windows SmartScreen's default prompt for new apps. Click 'More info', then 'Run anyway'. Kaitu is EV code-signed and this prompt won't appear after installation."
  },
  "macosGatekeeper": {
    "question": "macOS says 'cannot verify the developer'?",
    "answer": "After first install, go to System Settings then Privacy and Security, find the Kaitu prompt at the bottom, and click 'Open Anyway'. Subsequent launches won't show this. For VPN mode, you'll be guided to allow the system extension."
  },
  "security": {
    "question": "Is Kaitu safe?",
    "answer": "Kaitu is Apple notarized and Windows EV code-signed. All installers are distributed via HTTPS with verifiable SHA-256 checksums. The k2 protocol core is planned for open source."
  }
}
```

- [ ] **Step 3: Copy en-US translations to en-GB and en-AU** (identical English)

- [ ] **Step 4: Update zh-TW/install.json** (Traditional Chinese equivalents)

- [ ] **Step 5: Copy zh-TW to zh-HK** (identical Traditional Chinese)

- [ ] **Step 6: Update ja/install.json** (Japanese translations)

- [ ] **Step 7: Run build to verify i18n completeness**

Run: `cd web && npx next build 2>&1 | head -50`
Expected: No missing translation key warnings

- [ ] **Step 8: Commit**

```bash
cd web && git add messages/
git commit -m "i18n(install): remove Waymaker keys, add Hero/install-steps/FAQ keys (all 7 locales)"
```

---

### Task 7: Rewrite `InstallClient.tsx` — Smart Hero + Grid + FAQ

The largest task — complete rewrite of the install client component.

**Files:**
- Modify: `web/src/app/[locale]/install/InstallClient.tsx`
- Create: `web/tests/install-client.test.ts`
- Read: `web/src/components/home/HeroSection.tsx` (reference for design system alignment)

- [ ] **Step 1: Write tests for the new InstallClient structure**

Create `web/tests/install-client.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';

// Standard mocks
vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ children, href }: { children: React.ReactNode; href: string }) =>
    <a href={href}>{children}</a>,
}));
vi.mock('lucide-react', () => new Proxy({}, {
  get: () => () => null,
}));
vi.mock('@/components/ui/button', () => ({
  Button: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));
vi.mock('@/components/ui/card', () => ({
  Card: ({ children, ...props }: any) => <div data-testid="card" {...props}>{children}</div>,
}));
vi.mock('@/components/ui/accordion', () => ({
  Accordion: ({ children }: any) => <div data-testid="accordion">{children}</div>,
  AccordionItem: ({ children }: any) => <div data-testid="accordion-item">{children}</div>,
  AccordionTrigger: ({ children }: any) => <div>{children}</div>,
  AccordionContent: ({ children }: any) => <div>{children}</div>,
}));
vi.mock('@/lib/device-detection', () => ({
  detectDevice: vi.fn().mockReturnValue({
    type: 'macos', name: 'Mac', isMobile: false, isDesktop: true, userAgent: 'mac',
  }),
  triggerDownload: vi.fn().mockReturnValue(true),
  openDownloadInNewTab: vi.fn(),
}));
vi.mock('@/lib/constants', () => ({
  CDN_PRIMARY: 'https://cdn.test',
  CDN_BACKUP: 'https://backup.test',
  getDownloadLinks: (v: string) => ({
    windows: { primary: `https://cdn/${v}/win.exe`, backup: '' },
    macos: { primary: `https://cdn/${v}/mac.pkg`, backup: '' },
    linux: { primary: `https://cdn/${v}/linux.AppImage`, backup: '' },
  }),
}));

describe('InstallClient', () => {
  it('renders without crashing with all props', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient
        betaVersion="0.4.0-beta.1"
        stableVersion="0.3.22"
        mobileLinks={{ ios: 'https://apps.apple.com/test', android: 'https://cdn/android.apk' }}
      />
    );
    expect(container.innerHTML).not.toBe('');
  });

  it('renders without crashing when mobileLinks is null', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient betaVersion="0.4.0-beta.1" stableVersion={null} mobileLinks={null} />
    );
    expect(container.innerHTML).not.toBe('');
  });

  it('does not contain any Waymaker text', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient
        betaVersion="0.4.0-beta.1"
        stableVersion="0.3.22"
        mobileLinks={{ ios: 'https://apps.apple.com/test', android: 'https://cdn/android.apk' }}
      />
    );
    expect(container.innerHTML).not.toContain('Waymaker');
    expect(container.innerHTML).not.toContain('waymaker');
  });

  it('renders FAQ accordion section', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient
        betaVersion="0.4.0-beta.1"
        stableVersion="0.3.22"
        mobileLinks={{ ios: 'https://apps.apple.com/test', android: 'https://cdn/android.apk' }}
      />
    );
    expect(container.querySelector('[data-testid="accordion"]')).not.toBeNull();
  });

  it('renders exactly 3 platform cards in grid (desktop only, no iOS/Android cards)', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient
        betaVersion="0.4.0-beta.1"
        stableVersion="0.3.22"
        mobileLinks={{ ios: 'https://apps.apple.com/test', android: 'https://cdn/android.apk' }}
      />
    );
    // No iOS/Android platform cards in the grid
    const allCards = container.querySelectorAll('[data-testid="card"]');
    const cardTexts = Array.from(allCards).map(c => c.textContent || '');
    const hasIosCard = cardTexts.some(t => t.includes('install.install.iosDevices'));
    expect(hasIosCard).toBe(false);
  });

  it('renders 4 FAQ items', async () => {
    const { default: InstallClient } = await import(
      '../src/app/[locale]/install/InstallClient'
    );
    const { container } = render(
      <InstallClient
        betaVersion="0.4.0-beta.1"
        stableVersion="0.3.22"
        mobileLinks={{ ios: 'https://apps.apple.com/test', android: 'https://cdn/android.apk' }}
      />
    );
    const items = container.querySelectorAll('[data-testid="accordion-item"]');
    expect(items.length).toBe(4);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run tests/install-client.test.ts`
Expected: FAIL — InstallClient doesn't accept `mobileLinks` prop yet

- [ ] **Step 3: Rewrite `InstallClient.tsx`**

Complete rewrite following the spec Sections 1-3. The implementer should:

1. Read the current `InstallClient.tsx` to understand the download state machine
2. Read `web/src/components/home/HeroSection.tsx` for design system reference
3. Read spec `docs/superpowers/specs/2026-03-17-install-page-redesign.md` Sections 1-3

Key structural changes:
- **Props**: Add `mobileLinks: { ios: string; android: string } | null`
- **Hero section**: Platform icon in `bg-primary/10 rounded-2xl p-3` container, `font-mono` h1, `size="lg"` CTA with download verb, download states inline (no separate Card blocks)
- **CLI block** (macOS/Linux): `bg-card rounded-lg border font-mono text-sm`, `$` prefix in `text-muted-foreground`, copy button `w-5 h-5`
- **Install steps**: Show on `downloadState === 'success'`, numbered circles `w-8 h-8 rounded-full bg-primary/10 text-primary font-mono`, content from i18n `install.installSteps.{platform}`
- **Linux Hero**: CLI as primary CTA, AppImage as outline secondary
- **Mobile Hero**: Direct App Store / APK link, no auto-download
- **Grid**: 3 columns only (Windows, macOS, Linux). Remove iOS/Android `PlatformCard`
- **Mobile links**: One line `text-sm text-muted-foreground` below grid with inline links
- **FAQ**: `Accordion` from `@/components/ui/accordion`, 4 items, auto-expand based on `device.type`
- **Delete**: All Waymaker-related rendering, 5-column grid, standalone status Card blocks

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run tests/install-client.test.ts`
Expected: ALL PASS

- [ ] **Step 5: Run full test suite**

Run: `cd web && npx vitest run`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
cd web && git add src/app/\\[locale\\]/install/InstallClient.tsx tests/install-client.test.ts
git commit -m "feat(install): rewrite InstallClient — Smart Hero, 3-col grid, FAQ accordion"
```

---

### Task 8: Build gate + verification

Final validation — everything must build, pass tests, and have zero stale references.

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd web && npx vitest run`
Expected: ALL PASS

- [ ] **Step 2: Run production build**

Run: `cd web && yarn build 2>&1 | tail -20`
Expected: Build succeeds with no errors

- [ ] **Step 3: Verify zero DOWNLOAD_LINKS references**

Run: `grep -r "DOWNLOAD_LINKS" web/src/`
Expected: No output

- [ ] **Step 4: Verify zero BETA_VERSION/DESKTOP_VERSION references**

Run: `grep -r "BETA_VERSION\|DESKTOP_VERSION\|NEXT_PUBLIC_BETA_VERSION\|NEXT_PUBLIC_DESKTOP_VERSION" web/src/ web/next.config.ts`
Expected: No output

- [ ] **Step 5: Verify zero Waymaker references in source and messages**

Run: `grep -ri "waymaker" web/src/ web/messages/`
Expected: No output

- [ ] **Step 6: Start dev server and visually verify**

Run: `cd web && yarn dev`

Check:
- `/install` — Hero detects device, shows CTA with version, CLI block visible for macOS
- Download countdown and state transitions work
- 3-col grid below Hero (Windows/macOS/Linux only)
- Mobile links as inline text below grid
- FAQ accordion with 4 items, relevant item auto-expanded
- `/s/TESTCODE` — Invite page loads, download buttons work

- [ ] **Step 7: Commit any cleanup**

```bash
git add -A && git commit -m "fix(install): post-build cleanup"
```
