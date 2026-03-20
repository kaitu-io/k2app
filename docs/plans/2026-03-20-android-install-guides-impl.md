# Android 品牌安装指南 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add brand-specific Android install guide tabs (Xiaomi, Huawei, OPPO·vivo, Desktop USB, Generic) below the download button on the /install page, with UA-based auto-selection.

**Architecture:** Data-driven approach with a standalone `AndroidGuides` component consuming brand guide definitions from a separate data file. The `desktopUsb` tab reuses the existing `DesktopUsbInstallGuide` component. UA detection selects the default tab. All text is i18n via next-intl.

**Tech Stack:** Next.js 15, React 19, TypeScript, Tailwind CSS 4, next-intl, existing `Tabs`/`TabsContent` from `@/components/ui/tabs`

**Spec:** `docs/plans/2026-03-20-android-install-guides.md`

---

### Task 1: Add i18n keys to zh-CN/install.json

**Files:**
- Modify: `web/messages/zh-CN/install.json`

- [ ] **Step 1: Add androidGuides keys**

Add inside the `"install"` object, after the `"faq"` block:

```json
"androidGuides": {
  "title": "安装疑难指南",
  "xiaomiLabel": "小米",
  "xiaomiStep1Title": "开启飞行模式",
  "xiaomiStep1Desc": "安装前开启飞行模式，避免小米安全中心联网云检测拦截安装",
  "xiaomiStep2Title": "允许安装未知来源",
  "xiaomiStep2Desc": "系统提示时，点击「设置」→ 开启「允许此来源的应用」",
  "xiaomiStep3Title": "安装 APK",
  "xiaomiStep3Desc": "打开下载的文件，点击「继续安装」→「安装」",
  "xiaomiStep4Title": "关闭飞行模式并打开应用",
  "xiaomiStep4Desc": "安装完成后关闭飞行模式，打开开途，允许 VPN 权限",
  "huaweiLabel": "华为",
  "huaweiStep1Title": "开启飞行模式",
  "huaweiStep1Desc": "安装前开启飞行模式，避免华为安全中心联网云检测拦截安装",
  "huaweiStep2Title": "允许安装未知来源",
  "huaweiStep2Desc": "系统提示时，点击「设置」→ 开启「允许此来源的应用」",
  "huaweiStep3Title": "安装 APK",
  "huaweiStep3Desc": "打开下载的文件，点击「安装」",
  "huaweiStep4Title": "关闭飞行模式并打开应用",
  "huaweiStep4Desc": "安装完成后关闭飞行模式，打开开途，允许 VPN 权限",
  "oppoVivoLabel": "OPPO·vivo",
  "oppoVivoStep1Title": "开启飞行模式",
  "oppoVivoStep1Desc": "安装前开启飞行模式，避免手机安全中心联网云检测拦截安装",
  "oppoVivoStep2Title": "允许安装未知来源",
  "oppoVivoStep2Desc": "系统提示时，点击「设置」→ 开启「允许此来源的应用」",
  "oppoVivoStep3Title": "安装 APK",
  "oppoVivoStep3Desc": "打开下载的文件，点击「安装」",
  "oppoVivoStep4Title": "关闭飞行模式并打开应用",
  "oppoVivoStep4Desc": "安装完成后关闭飞行模式，打开开途，允许 VPN 权限",
  "desktopUsbLabel": "电脑辅助安装",
  "genericLabel": "通用安装",
  "genericStep1Title": "下载 APK",
  "genericStep1Desc": "点击上方下载按钮获取安装包，浏览器提示时选择「保留」",
  "genericStep2Title": "开启飞行模式",
  "genericStep2Desc": "安装前开启飞行模式，避免手机安全中心联网云检测拦截安装",
  "genericStep3Title": "允许安装未知来源",
  "genericStep3Desc": "系统提示时，点击「设置」→ 开启「允许此来源的应用」",
  "genericStep4Title": "安装 APK",
  "genericStep4Desc": "打开下载的文件，点击「安装」",
  "genericStep5Title": "关闭飞行模式并打开应用",
  "genericStep5Desc": "安装完成后关闭飞行模式，打开开途，允许 VPN 权限"
}
```

- [ ] **Step 2: Verify JSON is valid**

Run: `cd web && node -e "JSON.parse(require('fs').readFileSync('messages/zh-CN/install.json','utf8')); console.log('OK')"`
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add web/messages/zh-CN/install.json
git commit -m "feat(web): add zh-CN i18n keys for Android brand install guides"
```

---

### Task 2: Add i18n keys to all other locales

**Files:**
- Modify: `web/messages/en-US/install.json`
- Modify: `web/messages/en-GB/install.json`
- Modify: `web/messages/en-AU/install.json`
- Modify: `web/messages/ja/install.json`
- Modify: `web/messages/zh-TW/install.json`
- Modify: `web/messages/zh-HK/install.json`

- [ ] **Step 1: Add English (en-US) keys**

Same structure as zh-CN but translated to English. Key example:

```json
"androidGuides": {
  "title": "Installation Troubleshooting Guide",
  "xiaomiLabel": "Xiaomi",
  "xiaomiStep1Title": "Enable Airplane Mode",
  "xiaomiStep1Desc": "Enable airplane mode before installing to prevent Xiaomi security center from blocking the installation",
  ...
  "genericLabel": "General",
  "genericStep1Title": "Download APK",
  "genericStep1Desc": "Tap the download button above, choose 'Keep' if the browser warns you",
  ...
}
```

- [ ] **Step 2: Copy en-US to en-GB and en-AU** (same English text)

- [ ] **Step 3: Add Japanese (ja) keys**

Translate to Japanese.

- [ ] **Step 4: Add zh-TW and zh-HK keys**

Convert zh-CN to Traditional Chinese.

- [ ] **Step 5: Validate all locale JSON files**

Run: `cd web && for f in messages/*/install.json; do node -e "JSON.parse(require('fs').readFileSync('$f','utf8'))" && echo "$f OK"; done`
Expected: All 7 files say OK.

- [ ] **Step 6: Commit**

```bash
git add web/messages/*/install.json
git commit -m "feat(web): add i18n keys for Android install guides (all locales)"
```

---

### Task 3: Create android-guides-data.ts

**Files:**
- Create: `web/src/app/[locale]/install/android-guides-data.ts`

- [ ] **Step 1: Create the data file**

```typescript
/* eslint-disable react/jsx-no-literals */

// ---------------------------------------------------------------------------
// Android brand install guide data — step definitions + UA detection
// ---------------------------------------------------------------------------

export interface GuideStep {
  /** Image path relative to /public, e.g. "/images/install/xiaomi/step1.png". Omit for text-only steps. */
  image?: string;
  /** i18n key for step title (under install.androidGuides namespace) */
  titleKey: string;
  /** i18n key for step description */
  descriptionKey: string;
}

export interface BrandGuide {
  id: string;
  labelKey: string;
  steps: GuideStep[];
}

export const BRAND_GUIDES: BrandGuide[] = [
  {
    id: "xiaomi",
    labelKey: "install.androidGuides.xiaomiLabel",
    steps: [
      { image: "/images/install/xiaomi/step1.png", titleKey: "install.androidGuides.xiaomiStep1Title", descriptionKey: "install.androidGuides.xiaomiStep1Desc" },
      { image: "/images/install/xiaomi/step2.png", titleKey: "install.androidGuides.xiaomiStep2Title", descriptionKey: "install.androidGuides.xiaomiStep2Desc" },
      { image: "/images/install/xiaomi/step3.png", titleKey: "install.androidGuides.xiaomiStep3Title", descriptionKey: "install.androidGuides.xiaomiStep3Desc" },
      { image: "/images/install/xiaomi/step4.png", titleKey: "install.androidGuides.xiaomiStep4Title", descriptionKey: "install.androidGuides.xiaomiStep4Desc" },
    ],
  },
  {
    id: "huawei",
    labelKey: "install.androidGuides.huaweiLabel",
    steps: [
      { image: "/images/install/huawei/step1.png", titleKey: "install.androidGuides.huaweiStep1Title", descriptionKey: "install.androidGuides.huaweiStep1Desc" },
      { image: "/images/install/huawei/step2.png", titleKey: "install.androidGuides.huaweiStep2Title", descriptionKey: "install.androidGuides.huaweiStep2Desc" },
      { image: "/images/install/huawei/step3.png", titleKey: "install.androidGuides.huaweiStep3Title", descriptionKey: "install.androidGuides.huaweiStep3Desc" },
      { image: "/images/install/huawei/step4.png", titleKey: "install.androidGuides.huaweiStep4Title", descriptionKey: "install.androidGuides.huaweiStep4Desc" },
    ],
  },
  {
    id: "oppoVivo",
    labelKey: "install.androidGuides.oppoVivoLabel",
    steps: [
      { image: "/images/install/oppo-vivo/step1.png", titleKey: "install.androidGuides.oppoVivoStep1Title", descriptionKey: "install.androidGuides.oppoVivoStep1Desc" },
      { image: "/images/install/oppo-vivo/step2.png", titleKey: "install.androidGuides.oppoVivoStep2Title", descriptionKey: "install.androidGuides.oppoVivoStep2Desc" },
      { image: "/images/install/oppo-vivo/step3.png", titleKey: "install.androidGuides.oppoVivoStep3Title", descriptionKey: "install.androidGuides.oppoVivoStep3Desc" },
      { image: "/images/install/oppo-vivo/step4.png", titleKey: "install.androidGuides.oppoVivoStep4Title", descriptionKey: "install.androidGuides.oppoVivoStep4Desc" },
    ],
  },
  {
    id: "desktopUsb",
    labelKey: "install.androidGuides.desktopUsbLabel",
    steps: [], // Renders existing DesktopUsbInstallGuide component instead
  },
  {
    id: "generic",
    labelKey: "install.androidGuides.genericLabel",
    steps: [
      { titleKey: "install.androidGuides.genericStep1Title", descriptionKey: "install.androidGuides.genericStep1Desc" },
      { titleKey: "install.androidGuides.genericStep2Title", descriptionKey: "install.androidGuides.genericStep2Desc" },
      { titleKey: "install.androidGuides.genericStep3Title", descriptionKey: "install.androidGuides.genericStep3Desc" },
      { titleKey: "install.androidGuides.genericStep4Title", descriptionKey: "install.androidGuides.genericStep4Desc" },
      { titleKey: "install.androidGuides.genericStep5Title", descriptionKey: "install.androidGuides.genericStep5Desc" },
    ],
  },
];

/**
 * Detect which brand tab to show by default based on User Agent.
 * - Android device with known brand → that brand's tab
 * - Android device with unknown brand → "generic"
 * - Non-Android (desktop, iOS) → "desktopUsb"
 */
export function detectDefaultTab(ua: string): string {
  const lower = ua.toLowerCase();
  const isAndroid = /android/.test(lower);

  if (!isAndroid) {
    return "desktopUsb";
  }

  if (/xiaomi|redmi|miui|poco/.test(lower)) return "xiaomi";
  if (/huawei|honor|hmscore/.test(lower)) return "huawei";
  if (/oppo|realme|oneplus/.test(lower)) return "oppoVivo";
  if (/vivo/.test(lower)) return "oppoVivo";

  return "generic";
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit src/app/\[locale\]/install/android-guides-data.ts 2>&1 | head -20`
Expected: No errors (or only unrelated existing errors).

- [ ] **Step 3: Commit**

```bash
git add web/src/app/\[locale\]/install/android-guides-data.ts
git commit -m "feat(web): add Android brand guide data + UA detection"
```

---

### Task 4: Create AndroidGuides component

**Files:**
- Create: `web/src/app/[locale]/install/android-guides.tsx`

- [ ] **Step 1: Create the component**

```typescript
/* eslint-disable react/jsx-no-literals */
"use client";

import { useState, useEffect } from 'react';
import Image from 'next/image';
import { Tabs, TabsContent } from '@/components/ui/tabs';
import { DesktopUsbInstallGuide } from './install-guides';
import { BRAND_GUIDES, detectDefaultTab, type GuideStep } from './android-guides-data';

// ---------------------------------------------------------------------------
// Brand tab bar — horizontal scroll on mobile, flex on desktop
// ---------------------------------------------------------------------------

function BrandTabBar({
  selected,
  onSelect,
  t,
}: {
  selected: string;
  onSelect: (id: string) => void;
  t: (key: string) => string;
}) {
  return (
    <div className="flex gap-2 overflow-x-auto pb-2 mb-4 -mx-1 px-1">
      {BRAND_GUIDES.map((brand) => (
        <button
          key={brand.id}
          onClick={() => onSelect(brand.id)}
          className={`whitespace-nowrap px-4 py-2 rounded-lg border text-sm font-medium transition-all shrink-0 ${
            selected === brand.id
              ? 'border-primary bg-primary/10 text-foreground shadow-sm'
              : 'border-transparent text-muted-foreground hover:bg-muted/50'
          }`}
        >
          {t(brand.labelKey)}
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Image with fallback placeholder
// ---------------------------------------------------------------------------

function StepImage({ src, stepNumber }: { src: string; stepNumber: number }) {
  const [hasError, setHasError] = useState(false);

  if (hasError) {
    return (
      <div className="bg-muted rounded-lg flex items-center justify-center w-full aspect-[4/3] md:w-[300px] md:shrink-0">
        <span className="text-2xl font-bold text-muted-foreground">{stepNumber}</span>
      </div>
    );
  }

  return (
    <div className="relative w-full aspect-[4/3] md:w-[300px] md:shrink-0 rounded-lg overflow-hidden">
      <Image
        src={src}
        alt={`Step ${stepNumber}`}
        fill
        className="object-contain"
        onError={() => setHasError(true)}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step list renderer
// ---------------------------------------------------------------------------

function GuideStepList({ steps, t }: { steps: GuideStep[]; t: (key: string) => string }) {
  return (
    <div className="space-y-6">
      {steps.map((step, index) => (
        <div
          key={step.titleKey}
          className={`flex gap-4 ${step.image ? 'flex-col md:flex-row' : ''}`}
        >
          {step.image && <StepImage src={step.image} stepNumber={index + 1} />}
          <div className="flex gap-3 items-start">
            <div className="bg-primary/10 text-primary font-bold text-sm w-7 h-7 rounded-full flex items-center justify-center shrink-0">
              {index + 1}
            </div>
            <div>
              <h4 className="font-semibold text-foreground text-sm">{t(step.titleKey)}</h4>
              <p className="text-muted-foreground text-sm mt-1">{t(step.descriptionKey)}</p>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function AndroidGuides({ t }: { t: (key: string) => string }) {
  const [selectedBrand, setSelectedBrand] = useState("generic");

  // Detect brand from UA on mount
  useEffect(() => {
    setSelectedBrand(detectDefaultTab(navigator.userAgent));
  }, []);

  return (
    <div className="mt-8 max-w-2xl mx-auto text-left">
      <h3 className="text-base font-semibold text-foreground mb-3">
        {t('install.androidGuides.title')}
      </h3>

      <BrandTabBar selected={selectedBrand} onSelect={setSelectedBrand} t={t} />

      <Tabs value={selectedBrand} onValueChange={setSelectedBrand}>
        {BRAND_GUIDES.map((brand) => (
          <TabsContent key={brand.id} value={brand.id}>
            {brand.id === 'desktopUsb' ? (
              <DesktopUsbInstallGuide />
            ) : (
              <GuideStepList steps={brand.steps} t={t} />
            )}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}
```

- [ ] **Step 2: Verify no TypeScript errors**

Run: `cd web && npx tsc --noEmit 2>&1 | grep android-guides | head -10`
Expected: No errors from android-guides files.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/\[locale\]/install/android-guides.tsx
git commit -m "feat(web): add AndroidGuides component with brand tabs + UA detection"
```

---

### Task 5: Integrate AndroidGuides into AndroidPanel

**Files:**
- Modify: `web/src/app/[locale]/install/platform-panels.tsx`

- [ ] **Step 1: Replace DesktopUsbInstallGuide with AndroidGuides in AndroidPanel**

In `platform-panels.tsx`:

1. Add import: `import { AndroidGuides } from './android-guides';`
2. Remove `DesktopUsbInstallGuide` from the existing imports (line 11) — BUT keep it imported in install-guides.tsx exports since android-guides.tsx uses it directly.
3. Replace the install guide section in `AndroidPanel` (lines 256-261):

**Before:**
```tsx
{/* Install guide */}
<div className="mt-8 max-w-xl mx-auto space-y-4 text-left">
  <DownloadTipCard title={t('install.install.faq.androidUsbInstall.question')}>
    <DesktopUsbInstallGuide />
  </DownloadTipCard>
</div>
```

**After:**
```tsx
{/* Brand install guides */}
<AndroidGuides t={t} />
```

4. Clean up unused import: Remove `DesktopUsbInstallGuide` from the import line if no longer used in this file.

- [ ] **Step 2: Verify the page renders**

Run: `cd web && yarn dev &` then check `http://localhost:3000/zh-CN/install?platform=android` in browser.
Expected: Android panel shows with brand tabs below download button.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/\[locale\]/install/platform-panels.tsx
git commit -m "feat(web): integrate AndroidGuides into AndroidPanel"
```

---

### Task 6: Create placeholder image directories

**Files:**
- Create: `web/public/images/install/xiaomi/.gitkeep`
- Create: `web/public/images/install/huawei/.gitkeep`
- Create: `web/public/images/install/oppo-vivo/.gitkeep`
- Create: `web/public/images/install/desktop-usb/.gitkeep`

- [ ] **Step 1: Create directories with .gitkeep**

```bash
mkdir -p web/public/images/install/{xiaomi,huawei,oppo-vivo,desktop-usb}
touch web/public/images/install/{xiaomi,huawei,oppo-vivo,desktop-usb}/.gitkeep
```

- [ ] **Step 2: Commit**

```bash
git add web/public/images/install/
git commit -m "chore(web): add placeholder directories for Android install guide images"
```

---

### Task 7: Visual verification + final cleanup

- [ ] **Step 1: Run dev server and test all tabs**

Run: `cd web && yarn dev`

Verify at `http://localhost:3000/zh-CN/install?platform=android`:
1. Brand tab bar visible below download button
2. All 5 tabs clickable and switch content
3. "通用安装" tab shows text-only steps
4. "电脑辅助安装" tab shows existing DesktopUsbInstallGuide iframe
5. Brand tabs show step list with image placeholders (gray boxes with numbers)
6. Mobile responsive: tabs scroll horizontally

- [ ] **Step 2: Test UA detection**

Open browser DevTools → Network conditions → set UA to a Xiaomi device string.
Reload page → Xiaomi tab should be auto-selected.

Reset UA → desktop → "电脑辅助安装" tab should be default.

- [ ] **Step 3: Run lint**

Run: `cd web && yarn lint`
Expected: No new lint errors.

- [ ] **Step 4: Run existing tests**

Run: `cd web && yarn test`
Expected: All existing tests pass.

- [ ] **Step 5: Final commit if any cleanup needed**

```bash
git add -A && git commit -m "fix(web): Android install guides cleanup"
```
