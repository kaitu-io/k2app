# Install Page Redesign — 双区分离 + 单一数据源

## Problem

当前 `/install` 页面存在 6 个系统性问题：

1. **主次不分**：5 平台等权重平铺，移动端"即将推出"占 2/5 黄金位置
2. **CLI 不可读**：9px 字号，12px 复制按钮，技术用户核心路径被边缘化
3. **下载按钮无动词**：纯版本号 `v0.4.0-beta.1`，缺乏行动引导
4. **引导断裂**：下载成功后缺少安装步骤（macOS 系统扩展、Windows SmartScreen）
5. **帮助信息太弱**：纯文本列表，无交互，无设备关联
6. **`DOWNLOAD_LINKS` 违反单一数据源**：build-time 硬编码链接 vs CDN latest.json，两套数据互相矛盾

## Solution

双区分离架构：Smart Hero（针对当前设备的保姆级引导）+ 全平台参考区。同时删除 `DOWNLOAD_LINKS` 常量，统一走 CDN latest.json。

## Design

### Section 1: Smart Hero（智能引导区）

Hero 根据 `device.type` 展示针对性内容。所有下载状态（ready/downloading/success/failed）收入 Hero 内部，不再是独立 Card 区块。

**布局结构**：

```
┌─────────────────────────────────────────────────┐
│  [平台图标 w-16 bg-primary/10 rounded-2xl p-3]  │
│                                                  │
│  h1: 安装 Kaitu — 为你的 {platform} 准备好了      │
│  subtitle: 最新版本 v0.4.0-beta.1 [Beta badge]   │
│                                                  │
│  ┌─ 主 CTA ──────────────────────────────────┐  │
│  │  [⬇ 下载安装包]  (size=lg, primary)        │  │
│  │  倒计时 / 下载中 / 成功 / 失败状态          │  │
│  └────────────────────────────────────────────┘  │
│                                                  │
│  ┌─ CLI（macOS/Linux only）───────────────────┐  │
│  │  "或通过终端一键安装"                        │  │
│  │  $ curl -fsSL https://kaitu.io/i/k2         │  │
│  │    | sudo bash                     [复制]   │  │
│  └────────────────────────────────────────────┘  │
│                                                  │
│  ┌─ 安装步骤（downloadState === 'success'）──┐  │
│  │  ① 打开下载的安装文件                       │  │
│  │  ② 按提示完成安装                           │  │
│  │  ③ 平台特定步骤（系统扩展/SmartScreen）      │  │
│  └────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

**样式规范**：

- 平台图标：`w-16 h-16`，容器 `bg-primary/10 rounded-2xl p-3`（对齐 FeaturesSection icon 风格）
- h1：`text-3xl sm:text-4xl font-bold font-mono`（对齐 homepage）
- 主 CTA 按钮：`size="lg"`，文字 `下载安装包 vX.Y.Z`（有动词）
- CLI 终端块：`bg-card rounded-lg border font-mono text-sm`，`$` 用 `text-muted-foreground`，命令用 `text-foreground`，复制按钮 `w-5 h-5`
- 安装步骤：序号圆圈 `w-8 h-8 rounded-full bg-primary/10 text-primary font-mono`，步骤文字 `text-sm`
- 状态全部在 Hero 内切换，不产生页面级 Card 跳动

**平台分支行为**：

| device.type | 主 CTA | CLI | 安装步骤 |
|------------|--------|-----|---------|
| windows | 下载 .exe | 不显示 | SmartScreen 放行 |
| macos | 下载 .pkg | 显示 | 允许系统扩展 |
| linux | CLI 为主 CTA | 主位置 | 运行 AppImage |
| ios | App Store 链接 | 不显示 | 不显示 |
| android | APK 下载链接 | 不显示 | 允许安装未知来源 |
| unknown | 不自动下载 | 不显示 | 不显示，直接展示全平台 grid |

**Linux 特殊处理**：CLI 提升到主 CTA 位置（`size="lg"` 的终端代码块），AppImage 下载降为 outline 次按钮。

**移动端访问**：Hero 直接展示对应平台的下载链接（iOS → App Store，Android → APK），品牌统一为"开途"，无 Waymaker 字样。无倒计时/自动下载。

### Section 2: 全平台下载 Grid

Hero 下方，作为"其他平台"参考区。

**桌面端 3 列 grid**：

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  Windows      │  │  苹果电脑     │  │  Linux       │
│  Windows 10/11│  │  macOS 12+   │  │  Ubuntu/...  │
│               │  │              │  │              │
│ [⬇ 下载 vX.Y] │  │ [⬇ 下载 vX.Y] │  │ [⬇ 下载 vX.Y] │
│               │  │ $ curl ...   │  │ $ curl ...   │
└──────────────┘  └──────────────┘  └──────────────┘
```

**样式规范**：

- Grid：`grid-cols-1 sm:grid-cols-3 gap-4`（对齐 FeaturesSection 的 3 列惯例）
- 卡片：复用 `PlatformCard` 组件，统一高度（`flex flex-col` + `mt-auto` 对齐底部按钮）
- 下载按钮：文字 `下载 vX.Y.Z`（有动词），`w-full`
- CLI 命令：`text-xs font-mono`（12px），复制按钮 `w-4 h-4`
- 当前设备高亮：`border-primary ring-1 ring-primary`

**移动端链接**：grid 下方一行文字。

```
text-sm text-muted-foreground:
  iPhone / iPad → [App Store 链接]  ·  安卓 → [APK 下载]
```

无卡片、无图标。品牌统一为"开途 iOS 版"/"开途安卓版"。

**Stable 备选 + 备用下载**：保留现有底部小字逻辑（beta 时显示 stable 链接，备用 CDN 下载链接，历史版本链接）。

### Section 3: FAQ 帮助区

替换现有纯文本 `<ul>` 列表。

**组件**：shadcn/ui Accordion（`@/components/ui/accordion`）。如不存在则通过 `npx shadcn@latest add accordion` 添加。

**条目**：

1. 浏览器拦截了下载怎么办？
2. Windows 提示"未知发布者"怎么办？（SmartScreen 放行步骤）
3. macOS 提示"无法验证开发者"怎么办？（系统偏好设置步骤）
4. 软件安全吗？

**行为**：根据 `device.type` 自动展开相关条目（macOS 用户展开 #3，Windows 用户展开 #2）。

**SEO/GEO**：添加 `FAQPage` JSON-LD 结构化数据，对齐 homepage 的 GEO 策略。

### Section 4: 数据流改造 — 删除 DOWNLOAD_LINKS

**核心原则**：CDN latest.json 是唯一数据源。

#### 4.1 删除 `DOWNLOAD_LINKS` 常量

从 `web/src/lib/constants.ts` 删除：

```diff
- export const DESKTOP_VERSION = process.env.NEXT_PUBLIC_DESKTOP_VERSION || '0.0.0';
- export const BETA_VERSION = process.env.NEXT_PUBLIC_BETA_VERSION || '0.0.0';
  ...
- export const DOWNLOAD_LINKS = {
-   windows: `${CDN_PRIMARY}/${BETA_VERSION}/Kaitu_${BETA_VERSION}_x64.exe`,
-   macos: `${CDN_PRIMARY}/${BETA_VERSION}/Kaitu_${BETA_VERSION}_universal.pkg`,
-   ios: 'https://apps.apple.com/app/id6448744655',
-   android: 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/0.0.0/Kaitu-0.0.0.apk',
- } as const;
```

保留 `CDN_PRIMARY`、`CDN_BACKUP`、`getDownloadLinks(version)`（纯 URL 拼接工具）。

同时清理 `web/next.config.ts`：删除 `NEXT_PUBLIC_BETA_VERSION` 和 `NEXT_PUBLIC_DESKTOP_VERSION` env 注入（L37-39）以及 `getRootPackageJson()` 辅助函数（L19-32）。这些仅用于 `DOWNLOAD_LINKS`，不再需要。

#### 4.2 新建 `lib/downloads.ts` — 共享 fetch 逻辑

```typescript
// web/src/lib/downloads.ts
import { CDN_PRIMARY, CDN_BACKUP, getDownloadLinks } from './constants';

// CDN base for mobile manifests (no /desktop suffix)
const CDN_MOBILE_BASES = [
  'https://dl.kaitu.io/kaitu',
  'https://d13jc1jqzlg4yt.cloudfront.net/kaitu',
];

export interface MobileLinks {
  ios: string;    // App Store URL
  android: string; // APK direct download URL
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

/**
 * Flat record for device-detection helpers (getPrimaryDownloadLink).
 * Constructed from AllDownloadLinks for backward compat.
 */
export function flattenToRecord(all: AllDownloadLinks): Record<string, string> {
  const ver = all.desktop.beta || all.desktop.stable;
  return {
    windows: ver?.links.windows.primary ?? '',
    macos: ver?.links.macos.primary ?? '',
    ios: all.mobile?.ios ?? '',
    android: all.mobile?.android ?? '',
  };
}

/** Fetch desktop version from CDN manifest. Revalidates every 5 min (ISR). */
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

/** Fetch mobile download links from CDN manifests. */
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
        // android.url is absolute URL (e.g. "https://d0.all7.cc/kaitu/android/0.4.0/Kaitu-0.4.0.apk")
        // ios.appstore_url is App Store link
        return {
          ios: ios.appstore_url,
          android: android.url,
        };
      }
    } catch {}
  }
  return null;
}

/** Fetch all download links. Used by install page and invite page server components. */
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

**关键设计决策**：
- `android.url` 是**绝对 URL**（confirmed: `"url": "https://d0.all7.cc/kaitu/android/0.4.0-beta.3/Kaitu-0.4.0-beta.3.apk"`），直接使用，不拼接 base。
- `getDownloadLinks(version)` 在内部被 `fetchAllDownloadLinks` 调用，职责明确：版本号 → 桌面端 URL。
- `flattenToRecord()` 适配器：将嵌套的 `AllDownloadLinks` 转为 `Record<string, string>`，兼容 `getPrimaryDownloadLink()` 等 device-detection 工具函数。

#### 4.3 Install 页面改造

`install/page.tsx`：删除本地 `fetchDesktopVersion`，改用 `fetchAllDownloadLinks()`。

```typescript
// install/page.tsx
import { fetchAllDownloadLinks } from '@/lib/downloads';

export default async function InstallPage({ params }) {
  const all = await fetchAllDownloadLinks();
  return (
    <InstallClient
      betaVersion={all.desktop.beta?.version ?? null}
      stableVersion={all.desktop.stable?.version ?? null}
      mobileLinks={all.mobile}
    />
  );
}
```

`InstallClient.tsx` props 扩展：

```typescript
interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
  mobileLinks: { ios: string; android: string } | null;
}
```

#### 4.4 Invite 页面改造

**约束**：`s/[code]/page.tsx` 当前是 `export const dynamic = 'force-static'`。`force-static` 下 server fetch 仅在 build 时执行一次，无法 ISR revalidate。

**方案**：将 `force-static` 改为 `export const revalidate = 300`（5 分钟 ISR），与 install 页面一致。Invite 页面本身需要动态 code 参数，ISR 比纯 SSG 更合理。Trade-off：首次访问新 invite code 时 cold start 稍慢（ISR on-demand），但下载链接始终是最新的。

```typescript
// s/[code]/page.tsx
import { fetchAllDownloadLinks, flattenToRecord } from '@/lib/downloads';

export const revalidate = 300; // 替换 force-static

export default async function InvitePage({ params }) {
  const all = await fetchAllDownloadLinks();
  return (
    <InviteClient
      code={code}
      downloadLinks={flattenToRecord(all)}
    />
  );
}
```

`InviteClient.tsx` 改造：
- 删除 `import { DOWNLOAD_LINKS } from '@/lib/constants'`
- 新增 prop `downloadLinks: Record<string, string>`
- `getPrimaryDownloadLink(downloadLinks)` 调用不变，类型兼容（`flattenToRecord` 产出 `Record<string, string>`）
- 所有 `DOWNLOAD_LINKS.windows` / `.macos` / `.ios` / `.android` 改为 `downloadLinks.windows` 等

#### 4.5 i18n 变更

**删除 key**（所有 7 个 locale 的 `install.json`）：
- `install.waymakerNote`
- `install.kaituMobileComingSoon`
- `install.mobileComingSoon`
- `install.platformDevelopment`
- `install.useDesktopVersion`
- `install.appStore`
- `install.downloadApk`

**新增 key**：
- `install.heroTitle.{windows,macos,linux,ios,android,unknown}` — Hero h1 平台文案
- `install.heroSubtitle.{ios,android}` — 移动端 Hero 副标题
- `install.downloadButton` — "下载安装包"
- `install.installSteps.title` — "安装步骤"
- `install.installSteps.macos.{1,2,3}` — macOS 安装步骤
- `install.installSteps.windows.{1,2,3}` — Windows 安装步骤
- `install.installSteps.linux.{1,2,3}` — Linux 安装步骤
- `install.installSteps.android.{1,2}` — Android 安装步骤
- `install.otherPlatforms` — "其他平台"
- `install.mobileDownloads` — "移动端"
- `install.kaituIos` — "开途 iOS 版"
- `install.kaituAndroid` — "开途安卓版"
- `install.terminalInstall` — "或通过终端一键安装"
- `install.faq.browserBlock.{question,answer}`
- `install.faq.windowsSmartScreen.{question,answer}`
- `install.faq.macosGatekeeper.{question,answer}`
- `install.faq.security.{question,answer}`

## Files Changed

| File | Change |
|------|--------|
| `web/src/lib/constants.ts` | 删除 `DOWNLOAD_LINKS`、`BETA_VERSION`、`DESKTOP_VERSION` |
| `web/src/lib/downloads.ts` | **新建** — `fetchAllDownloadLinks()`、`flattenToRecord()`、`MobileLinks`/`AllDownloadLinks` 类型 |
| `web/src/lib/device-detection.ts` | 无变更（`getPrimaryDownloadLink` 签名 `Record<string, string>` 不变） |
| `web/src/app/[locale]/install/page.tsx` | 用 `fetchAllDownloadLinks()` 替代本地 `fetchDesktopVersion()`，传 `mobileLinks` 给 client |
| `web/src/app/[locale]/install/InstallClient.tsx` | 重写：Smart Hero + 3 列 grid + 移动端链接行 + FAQ Accordion |
| `web/src/app/[locale]/s/[code]/page.tsx` | `force-static` → `revalidate = 300`，调用 `fetchAllDownloadLinks()`，传 `flattenToRecord(all)` 给 InviteClient |
| `web/src/app/[locale]/s/[code]/InviteClient.tsx` | 新增 `downloadLinks` prop，删除 `DOWNLOAD_LINKS` import，所有引用改为 prop |
| `web/src/components/ui/accordion.tsx` | **新建** — `npx shadcn@latest add accordion`（如不存在） |
| `web/next.config.ts` | 删除 `getRootPackageJson()`、`desktopVersion`/`betaVersion` 变量、`NEXT_PUBLIC_*` env 注入 |
| `web/messages/zh-CN/install.json` | 删除 waymaker 相关 key，新增 Hero/安装步骤/FAQ key |
| `web/messages/{en-US,en-GB,en-AU,zh-TW,zh-HK,ja}/install.json` | 同步所有新增/删除 key |
| `web/tests/complex-pages-ssr.test.ts` | 更新 mock：删除 `DESKTOP_VERSION` 相关 mock |

## Verification

- `cd web && yarn build` — 无编译错误
- `cd web && yarn test` — 现有测试通过（需更新 test mock）
- 手动验证：访问 `/install`，确认 Hero 检测设备并展示正确链接
- 手动验证：访问 `/s/{code}` invite 页面，确认下载链接正常
- CDN latest.json ISR revalidate（5min）：修改 S3 manifest 后 5 分钟内页面反映新版本
- 确认 `DOWNLOAD_LINKS` 零引用：`grep -r "DOWNLOAD_LINKS" web/src/` 无结果
