# Homepage Optimization Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Overhaul the web/ homepage: remove comparison table, split monolithic page.tsx into focused components, unify styling to Tailwind (eliminate inline styles), replace emoji with Lucide icons, streamline download section to link to /install, update CTAs, enhance Header navigation, and integrate the k2cc pulse animation background.

**Architecture:** Server Component page.tsx becomes a thin compositor importing 3 section components (HeroSection, FeaturesSection, DownloadCTA). The k2cc pulse Canvas is a fixed client-side background layer rendered via HomeClient.tsx. All inline `style={{}}` props replaced with Tailwind utility classes using existing CSS variable mappings in globals.css. i18n keys updated across all 7 locales.

**Tech Stack:** Next.js 15 (App Router), React 19, Tailwind CSS 4, shadcn/ui, next-intl, Lucide React, Canvas 2D API, Web Audio API

---

## Key Design Decisions

### Download Section → /install CTA

**Decision:** Replace the 4-card download grid with a compact CTA section linking to `/install`, and add a "Download" button in Header.

**Rationale:**
- `/install` already has superior UX: device detection, auto-download countdown, all platforms grid, backup CDN links, version display
- SEO: single canonical download page (`/install`), no duplicate content across homepage and install page
- Maintenance: version updates only happen in one place (CDN manifest fetch in install page)
- The homepage download cards were showing hardcoded `BETA_VERSION` from build time, while `/install` fetches live versions from CDN

### CTA Buttons

- Primary: "我要买" (zh) / "Subscribe" (en) — direct purchase intent, serves existing users needing renewal
- Secondary: "下载客户端" (zh) / "Download" (en) — links to `/install`

### Button Hover Fix (existing bug)

**Bug:** Current homepage buttons use inline `style={{ backgroundColor: 'var(--primary)' }}` which has higher CSS specificity than the Button component's Tailwind `hover:bg-primary/90` class. Result: hover does nothing.

**Fix:** Remove all inline `style={{}}` from buttons. Use Tailwind classes only. The default variant already has `hover:bg-primary/90`. For `variant="outline"` buttons with custom border/text colors, we add explicit hover overrides (`hover:bg-secondary/10 hover:text-secondary`) because the variant's default `hover:bg-accent hover:text-accent-foreground` would turn a cyan-themed button into a green one.

### Feature Icons: Emoji → Lucide

| Feature | Emoji | Lucide Icon |
|---------|-------|-------------|
| k2cc Congestion | 📈 | `TrendingUp` |
| ECH Stealth | 🛡️ | `ShieldCheck` |
| QUIC+TCP-WS | 🔀 | `Shuffle` |
| Zero Deploy | ⚡ | `Zap` |
| Reverse Proxy | 🎭 | `EyeOff` |
| Self-Signed Cert | 🔐 | `Lock` |

---

## Phase 1: Homepage Cleanup & Optimization

### Task 1: Update i18n — CTA text & Header nav keys (all 7 locales)

**Files:**
- Modify: `web/messages/zh-CN/hero.json`
- Modify: `web/messages/en-US/hero.json`
- Modify: `web/messages/zh-TW/hero.json`
- Modify: `web/messages/zh-HK/hero.json`
- Modify: `web/messages/en-GB/hero.json`
- Modify: `web/messages/en-AU/hero.json`
- Modify: `web/messages/ja/hero.json`
- Modify: `web/messages/zh-CN/nav.json`
- Modify: `web/messages/en-US/nav.json`
- Modify: `web/messages/zh-TW/nav.json`
- Modify: `web/messages/zh-HK/nav.json`
- Modify: `web/messages/en-GB/nav.json`
- Modify: `web/messages/en-AU/nav.json`
- Modify: `web/messages/ja/nav.json`

**Context:** All 7 locales must have identical key structures. zh-CN is primary, en-US secondary. The other 5 locales (zh-TW, zh-HK, en-GB, en-AU, ja) follow their respective base language.

- [ ] **Step 1: Update zh-CN hero.json — CTA text + download CTA section**

In `web/messages/zh-CN/hero.json`:
- Change `hero.cta_primary` from `"开通和续费"` to `"我要买"`
- Add new key `hero.downloadCta.title` = `"下载客户端"`
- Add new key `hero.downloadCta.subtitle` = `"支持 Windows、macOS、Linux、iOS、Android 全平台"`
- Add new key `hero.downloadCta.button` = `"前往下载页"`
- Add new key `hero.downloadCta.platforms` = `"Windows · macOS · Linux · iOS · Android"`

- [ ] **Step 2: Update en-US hero.json — same key changes**

- Change `hero.cta_primary` to `"Subscribe"`
- Add `hero.downloadCta.title` = `"Download Client"`
- Add `hero.downloadCta.subtitle` = `"Available on Windows, macOS, Linux, iOS, and Android"`
- Add `hero.downloadCta.button` = `"Go to Downloads"`
- Add `hero.downloadCta.platforms` = `"Windows · macOS · Linux · iOS · Android"`

- [ ] **Step 3: Update remaining 5 locale hero.json files with same keys**

zh-TW: `"我要買"`, `"下載客戶端"`, `"支持 Windows、macOS、Linux、iOS、Android 全平台"`, `"前往下載頁"`, `"Windows · macOS · Linux · iOS · Android"`
zh-HK: same as zh-TW
en-GB: same as en-US
en-AU: same as en-US
ja: `"購入する"`, `"クライアントをダウンロード"`, `"Windows・macOS・Linux・iOS・Android 全プラットフォーム対応"`, `"ダウンロードページへ"`, `"Windows · macOS · Linux · iOS · Android"`

- [ ] **Step 4: Update zh-CN nav.json — add Header nav keys**

In `web/messages/zh-CN/nav.json`, add to `nav`:
```json
"download": "下载",
"quickstart": "快速开始"
```

- [ ] **Step 5: Update en-US nav.json — same Header nav keys**

```json
"download": "Download",
"quickstart": "Quick Start"
```

- [ ] **Step 6: Update remaining 5 locale nav.json files**

zh-TW: `"下載"`, `"快速開始"`
zh-HK: same as zh-TW
en-GB: same as en-US
en-AU: same as en-US
ja: `"ダウンロード"`, `"クイックスタート"`

- [ ] **Step 7: Verify all locale files parse as valid JSON**

Run: `cd web && node -e "const fs=require('fs'); const locales=['zh-CN','en-US','zh-TW','zh-HK','en-GB','en-AU','ja']; const ns=['hero','nav']; for(const l of locales) for(const n of ns) { JSON.parse(fs.readFileSync('messages/'+l+'/'+n+'.json','utf8')); console.log(l+'/'+n+' OK'); }"`

Expected: All 14 files print "OK", no parse errors.

- [ ] **Step 8: Commit**

```bash
git add web/messages/
git commit -m "feat(web): update i18n — CTA text, header nav keys, download CTA section (7 locales)"
```

---

### Task 2: Create HeroSection component

**Files:**
- Create: `web/src/components/home/HeroSection.tsx`
- Reference: `web/src/app/[locale]/page.tsx` (current hero markup, lines 277-341)

**Context:** This is a Server Component. It receives translated strings as props (no `useTranslations` — parent passes `t` function results). All inline styles must be converted to Tailwind classes. The CSS variable mappings already exist in `globals.css` — e.g. `--primary: #00ff88` is mapped to `--color-primary` which means Tailwind `text-primary`, `bg-primary`, `border-primary` all work.

- [ ] **Step 1: Create HeroSection.tsx**

Create `web/src/components/home/HeroSection.tsx`:

```tsx
import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, Zap } from 'lucide-react';

interface HeroSectionProps {
  title: string;
  subtitle: string;
  description: string;
  ctaPrimary: string;
  ctaSecondary: string;
  terminalTitle: string;
}

export default function HeroSection({
  title,
  subtitle,
  description,
  ctaPrimary,
  ctaSecondary,
  terminalTitle,
}: HeroSectionProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto text-center">
        {/* Badge */}
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs mb-6 bg-primary/10 text-primary border border-primary/30 font-mono">
          <span className="w-2 h-2 rounded-full animate-pulse bg-primary" />
          k2v5 — k2cc Anti-QoS Congestion Control
        </div>

        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold mb-6 leading-tight font-mono text-foreground">
          {title}
        </h1>

        <p className="text-xl mb-4 max-w-3xl mx-auto text-secondary font-mono">
          {subtitle}
        </p>

        <p className="text-base mb-10 max-w-3xl mx-auto text-muted-foreground">
          {description}
        </p>

        {/* CTAs */}
        <div className="flex flex-col sm:flex-row justify-center items-center gap-4 max-w-md sm:max-w-2xl mx-auto">
          <Link href="/purchase" className="w-full sm:flex-1">
            <Button size="lg" className="w-full min-w-[200px] font-bold bg-primary text-primary-foreground font-mono">
              <Zap className="w-5 h-5 mr-2" />
              {ctaPrimary}
            </Button>
          </Link>
          <Link href="/install" className="w-full sm:flex-1">
            <Button variant="outline" size="lg" className="w-full min-w-[200px] border-secondary text-secondary hover:bg-secondary/10 hover:text-secondary font-mono">
              <Download className="w-5 h-5 mr-2" />
              {ctaSecondary}
            </Button>
          </Link>
        </div>

        {/* Terminal preview */}
        <div className="mt-14 max-w-2xl mx-auto rounded-lg overflow-hidden text-left bg-card border border-primary/20">
          <div className="flex items-center gap-2 px-4 py-3 bg-primary/5 border-b border-primary/10">
            <span className="w-3 h-3 rounded-full bg-red-500 opacity-70" />
            <span className="w-3 h-3 rounded-full bg-yellow-500 opacity-70" />
            <span className="w-3 h-3 rounded-full bg-primary opacity-70" />
            <span className="ml-2 text-xs text-muted-foreground font-mono">
              k2s — {terminalTitle}
            </span>
          </div>
          <div className="p-6 text-sm space-y-2 font-mono">
            <div>
              <span className="text-muted-foreground">$ </span>
              <span className="text-primary">curl -fsSL https://kaitu.io/i/k2s | sudo sh</span>
            </div>
            <div className="text-muted-foreground">Installing k2s...</div>
            <div className="text-secondary">[k2s] ECH stealth tunnel started on :443</div>
            <div className="text-secondary">[k2s] Connection URI:</div>
            <div className="break-all text-primary">k2v5://Zt8x...@your-server:443</div>
          </div>
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Verify file was created correctly**

Run: `head -5 web/src/components/home/HeroSection.tsx`

Expected: Shows the import statements.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/home/HeroSection.tsx
git commit -m "feat(web): create HeroSection component with Tailwind-only styling"
```

---

### Task 3: Create FeaturesSection component with Lucide icons

**Files:**
- Create: `web/src/components/home/FeaturesSection.tsx`
- Reference: `web/src/app/[locale]/page.tsx` (feature cards, lines 343-375)

**Context:** Replace all 6 emoji icons with Lucide React components. Use Tailwind classes only — no inline styles. The icon background container uses `bg-primary/10`. Border-top color alternates between `border-t-primary` and `border-t-secondary`.

- [ ] **Step 1: Create FeaturesSection.tsx**

Create `web/src/components/home/FeaturesSection.tsx`:

```tsx
import { Card } from '@/components/ui/card';
import {
  TrendingUp,
  ShieldCheck,
  Shuffle,
  Zap,
  EyeOff,
  Lock,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

interface FeaturesSectionProps {
  sectionTitle: string;
  features: {
    congestion: { title: string; description: string };
    ech: { title: string; description: string };
    transport: { title: string; description: string };
    zeroDeploy: { title: string; description: string };
    reverseProxy: { title: string; description: string };
    selfSign: { title: string; description: string };
  };
}

const ICON_MAP: Record<string, { icon: LucideIcon; accent: 'primary' | 'secondary' }> = {
  congestion: { icon: TrendingUp, accent: 'primary' },
  ech: { icon: ShieldCheck, accent: 'secondary' },
  transport: { icon: Shuffle, accent: 'primary' },
  zeroDeploy: { icon: Zap, accent: 'secondary' },
  reverseProxy: { icon: EyeOff, accent: 'primary' },
  selfSign: { icon: Lock, accent: 'secondary' },
};

const FEATURE_ORDER = ['congestion', 'ech', 'transport', 'zeroDeploy', 'reverseProxy', 'selfSign'] as const;

export default function FeaturesSection({ sectionTitle, features }: FeaturesSectionProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8 bg-[rgba(5,5,8,0.6)] backdrop-blur-sm">
      <div className="max-w-7xl mx-auto">
        <div className="text-center mb-14">
          <h2 className="text-3xl font-bold font-mono">{sectionTitle}</h2>
        </div>

        <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
          {FEATURE_ORDER.map((key) => {
            const { icon: Icon, accent } = ICON_MAP[key];
            const feature = features[key];
            return (
              <Card
                key={key}
                className={`p-6 transition-all duration-300 hover:shadow-lg border-t-4 bg-card ${
                  accent === 'primary' ? 'border-t-primary' : 'border-t-secondary'
                }`}
              >
                <div className="w-12 h-12 mb-4 rounded-lg flex items-center justify-center bg-primary/10">
                  <Icon className={`w-6 h-6 ${
                    accent === 'primary' ? 'text-primary' : 'text-secondary'
                  }`} />
                </div>
                <h4 className="font-bold mb-2 text-foreground font-mono">{feature.title}</h4>
                <p className="text-sm leading-relaxed text-muted-foreground">{feature.description}</p>
              </Card>
            );
          })}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/home/FeaturesSection.tsx
git commit -m "feat(web): create FeaturesSection component — emoji replaced with Lucide icons"
```

---

### Task 4: Create DownloadCTA component (links to /install)

**Files:**
- Create: `web/src/components/home/DownloadCTA.tsx`

**Context:** Replaces the old 4-card download grid. Simple CTA section with a heading, subtitle listing all platforms, and a single button linking to `/install`. Linux is now listed as a supported platform. No direct CDN download links — all download logic lives in `/install`.

- [ ] **Step 1: Create DownloadCTA.tsx**

Create `web/src/components/home/DownloadCTA.tsx`:

```tsx
import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, Monitor, Smartphone } from 'lucide-react';

interface DownloadCTAProps {
  title: string;
  subtitle: string;
  buttonText: string;
  platforms: string;
}

export default function DownloadCTA({ title, subtitle, buttonText, platforms }: DownloadCTAProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-3xl mx-auto text-center">
        <div className="flex justify-center gap-4 mb-6">
          <Monitor className="w-8 h-8 text-primary" />
          <Smartphone className="w-8 h-8 text-secondary" />
        </div>
        <h2 className="text-3xl font-bold mb-4 font-mono">{title}</h2>
        <p className="text-muted-foreground mb-3">{subtitle}</p>
        <p className="text-sm text-muted-foreground/70 mb-8 font-mono">{platforms}</p>
        <Link href="/install">
          <Button size="lg" className="font-bold bg-primary text-primary-foreground font-mono">
            <Download className="w-5 h-5 mr-2" />
            {buttonText}
          </Button>
        </Link>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/home/DownloadCTA.tsx
git commit -m "feat(web): create DownloadCTA component — links to /install, includes Linux"
```

---

### Task 5: Update Header — add Download button and navigation links

**Files:**
- Modify: `web/src/components/Header.tsx`

**Context:** Header is a client component (`"use client"`). Add a Download button (small, outlined, primary color) in the nav area. Add "Quick Start" link alongside existing K2 Protocol / Routers / GitHub links. The Download button should link to `/install`. Use `Link` from `@/i18n/routing` for all internal links.

- [ ] **Step 1: Update Header.tsx — add Download button and Quick Start link**

In `web/src/components/Header.tsx`:

1. Add `Download` import from `lucide-react` (alongside existing `Github`):
```tsx
import { Github, Download } from 'lucide-react';
```

2. Replace the existing nav links section (the `hidden sm:flex` div, lines 34-56) with:

```tsx
<div className="hidden sm:flex items-center space-x-3">
  <Link
    href="/k2"
    className="text-sm text-muted-foreground hover:text-foreground transition-colors"
  >
    {t('nav.nav.k2Protocol')}
  </Link>
  <div className="w-px h-4 bg-border" />
  <Link
    href="/k2/quickstart"
    className="text-sm text-muted-foreground hover:text-foreground transition-colors"
  >
    {t('nav.nav.quickstart')}
  </Link>
  <div className="w-px h-4 bg-border" />
  <Link
    href="/routers"
    className="text-sm text-muted-foreground hover:text-foreground transition-colors"
  >
    {t('nav.nav.routers')}
  </Link>
  <div className="w-px h-4 bg-border" />
  <Link
    href="/opensource"
    className="text-muted-foreground hover:text-foreground transition-colors"
    title={t('nav.nav.openSource')}
  >
    <Github className="w-5 h-5" />
  </Link>
  <div className="w-px h-4 bg-border" />
  <Link href="/install">
    <Button variant="outline" size="sm" className="border-primary text-primary hover:bg-primary/10 hover:text-primary font-mono text-xs">
      <Download className="w-3.5 h-3.5 mr-1" />
      {t('nav.nav.download')}
    </Button>
  </Link>
</div>
```

- [ ] **Step 2: Verify the Header renders without errors**

Run: `cd web && npx next build 2>&1 | head -30`

If build passes without errors in Header, proceed. If i18n key errors appear, verify nav.json files from Task 1.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Header.tsx
git commit -m "feat(web): add Download button and Quick Start link to Header nav"
```

---

### Task 6: Rewrite page.tsx — compose new components, remove comparison table

**Files:**
- Modify: `web/src/app/[locale]/page.tsx`

**Context:** This is the main integration task. The page.tsx becomes a thin Server Component that:
1. Fetches translations
2. Passes props to HeroSection, FeaturesSection, DownloadCTA
3. Removes the entire comparison table section (lines 378-441) and its data (lines 99-213)
4. Removes the old download section (lines 443-548)
5. Removes unused imports (`ExternalLink`, `Smartphone`, `Monitor`, `Card`, `DOWNLOAD_LINKS`, `NextLink`)

- [ ] **Step 1: Rewrite page.tsx**

Replace the entire content of `web/src/app/[locale]/page.tsx` with:

```tsx
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import HeroSection from '@/components/home/HeroSection';
import FeaturesSection from '@/components/home/FeaturesSection';
import DownloadCTA from '@/components/home/DownloadCTA';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'hero' });

  return {
    title: `${t('hero.title')} Kaitu k2`,
    description: t('hero.description'),
  };
}

const JSON_LD_CONTENT = JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'SoftwareApplication',
  name: 'Kaitu k2',
  applicationCategory: 'NetworkingApplication',
  operatingSystem: 'Windows, macOS, iOS, Android, Linux',
  description:
    'ECH-based stealth tunnel protocol powered by k2cc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
  url: 'https://kaitu.io',
  publisher: {
    '@type': 'Organization',
    name: 'Kaitu',
    url: 'https://kaitu.io',
  },
  offers: {
    '@type': 'Offer',
    price: '0',
    priceCurrency: 'USD',
  },
  featureList: [
    'ECH (Encrypted Client Hello) stealth',
    'QUIC + TCP-WebSocket dual-stack transport',
    'k2cc adaptive rate control',
    'Reverse proxy camouflage',
    'Self-signed certificate + certificate pinning',
    'Zero CT log exposure',
    'One-command deployment',
  ],
});

export default async function Home({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'hero' });

  return (
    <div className="min-h-screen text-foreground" style={{ backgroundColor: '#050508' }}>
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON_LD_CONTENT }}
      />
      <Header />

      <HeroSection
        title={t('hero.title')}
        subtitle={t('hero.subtitle')}
        description={t('hero.description')}
        ctaPrimary={t('hero.cta_primary')}
        ctaSecondary={t('hero.cta_secondary')}
        terminalTitle={t('hero.terminalTitle')}
      />

      <FeaturesSection
        sectionTitle={t('hero.features.title')}
        features={{
          congestion: {
            title: t('hero.features.congestion.title'),
            description: t('hero.features.congestion.description'),
          },
          ech: {
            title: t('hero.features.ech.title'),
            description: t('hero.features.ech.description'),
          },
          transport: {
            title: t('hero.features.transport.title'),
            description: t('hero.features.transport.description'),
          },
          zeroDeploy: {
            title: t('hero.features.zeroDeploy.title'),
            description: t('hero.features.zeroDeploy.description'),
          },
          reverseProxy: {
            title: t('hero.features.reverseProxy.title'),
            description: t('hero.features.reverseProxy.description'),
          },
          selfSign: {
            title: t('hero.features.selfSign.title'),
            description: t('hero.features.selfSign.description'),
          },
        }}
      />

      <DownloadCTA
        title={t('hero.downloadCta.title')}
        subtitle={t('hero.downloadCta.subtitle')}
        buttonText={t('hero.downloadCta.button')}
        platforms={t('hero.downloadCta.platforms')}
      />

      <Footer />
    </div>
  );
}
```

Note: The `dangerouslySetInnerHTML` for JSON-LD is safe — content is a hardcoded constant string, not user input.

- [ ] **Step 2: Fix homepage-ssr.test.ts — add HomeClient mock**

In `web/tests/homepage-ssr.test.ts`, add a mock for the new HomeClient import (after the existing Footer mock):

```typescript
// Mock HomeClient (canvas animation — no SSR)
vi.mock('../src/app/[locale]/HomeClient', () => ({
  default: () => null,
}));

// Mock new section components
vi.mock('@/components/home/HeroSection', () => ({
  default: () => null,
}));

vi.mock('@/components/home/FeaturesSection', () => ({
  default: () => null,
}));

vi.mock('@/components/home/DownloadCTA', () => ({
  default: () => null,
}));
```

Also remove the now-unused mocks for `@/lib/constants` (DOWNLOAD_LINKS) and `@/components/ui/card` since page.tsx no longer imports them directly.

- [ ] **Step 3: Verify build succeeds**

Run: `cd web && npx next build 2>&1 | tail -20`

Expected: Build succeeds. No TypeScript errors, no missing i18n keys.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/[locale]/page.tsx
git commit -m "feat(web): rewrite homepage — compose section components, remove comparison table"
```

---

### Task 7: Visual verification

**Files:** None (verification only)

- [ ] **Step 1: Start dev server and check homepage**

Run: `cd web && yarn dev`

Open `http://localhost:3000/zh-CN` in browser. Verify:
1. Hero section renders with "我要买" primary CTA and "下载客户端" secondary CTA
2. **Button hover works**: primary button darkens on hover (`bg-primary/90`), outline button shows subtle cyan tint (`bg-secondary/10`)
3. Badge shows "k2v5 — k2cc Anti-QoS Congestion Control" with pulse dot
4. Terminal box renders with green text
5. Features section shows 6 cards with Lucide icons (no emoji)
6. Each card has colored top border (alternating primary/secondary)
7. Download CTA section shows "下载客户端" heading with button linking to /install
8. Header shows new nav links: Quick Start, Download button — Download button hover shows green tint
9. No comparison table visible
10. Footer renders correctly

- [ ] **Step 2: Check en-US locale**

Open `http://localhost:3000/en-US`. Verify:
1. CTA says "Subscribe" / "Download Client"
2. All section titles in English
3. Download CTA lists all 5 platforms including Linux

- [ ] **Step 3: Check mobile viewport (Chrome DevTools)**

Open Chrome DevTools → Device toolbar → iPhone 14. Verify:
1. Hero CTAs stack vertically
2. Feature cards become single column on mobile
3. Header collapses nav links (only language switcher + auth visible)
4. Download CTA section readable on small screens

---

## Phase 2: k2cc Pulse Animation

**Spec reference:** `docs/superpowers/specs/2026-03-17-k2cc-hero-pulse-animation.md`

The animation is a full-page fixed Canvas background with a 6-beat energy curve driven by scroll progress. It renders: ECG heartbeat waveform → glitch/noise → silence → buildup with branches/particles → Tesla coil lightning burst → aftermath decay. Zero dependencies, pure Canvas 2D + Web Audio.

### Task 8: Create animation type definitions and constants

**Files:**
- Create: `web/src/components/k2cc-hero/types.ts`
- Create: `web/src/components/k2cc-hero/constants.ts`

- [ ] **Step 1: Create types.ts**

Create `web/src/components/k2cc-hero/types.ts`:

```typescript
export interface Particle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number;       // 1.0 (born) → 0.0 (dead)
  decay: number;      // per-frame decay (0.01-0.04)
  size: number;       // radius px
  brightness: number; // 0-1
  active: boolean;
}

export interface BoltSegment {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  width: number;
  brightness: number;
  depth: number;
}

export interface EnergyParams {
  amplitude: number;
  frequency: number;
  glowRadius: number;
  color: string;
  noiseIntensity: number;
  lineWidth: number;
}

export interface GlitchState {
  phase: 'idle' | 'triggered' | 'active' | 'cooldown';
  framesLeft: number;
  cooldownLeft: number;
  offset: number;
  width: number;
}

export type BeatName = 'rest' | 'sense' | 'silence' | 'buildup' | 'burst' | 'aftermath';

export interface RenderState {
  scrollProgress: number;
  smoothProgress: number;
  scrollDirection: 'down' | 'up';
  time: number;
  beat: BeatName;
  energy: EnergyParams;
  glitch: GlitchState;
  particles: Particle[];
  shakeX: number;
  shakeY: number;
  hasPlayedSound: boolean;
  wordmarkOpacity: number;
}
```

- [ ] **Step 2: Create constants.ts**

Create `web/src/components/k2cc-hero/constants.ts`:

```typescript
// Colors
export const COLOR_PRIMARY = '#00ff88';
export const COLOR_SECONDARY = '#00d4ff';
export const COLOR_SILENCE = '#005533';
export const COLOR_BACKGROUND = '#050508';
export const COLOR_WHITE = '#ffffff';

// Beat boundaries (scrollProgress)
export const BEAT_REST_END = 0.20;
export const BEAT_SENSE_END = 0.35;
export const BEAT_SILENCE_END = 0.45;
export const BEAT_BUILDUP_END = 0.65;
export const BEAT_BURST_END = 0.80;
// 0.80–1.00 = aftermath

// Performance
export const MAX_DPR_DESKTOP = 2;
export const MAX_DPR_MOBILE = 2;
export const PARTICLE_POOL_DESKTOP = 50;
export const PARTICLE_POOL_MOBILE = 20;
export const ARC_COUNT_DESKTOP = 8;
export const ARC_COUNT_TABLET = 5;
export const ARC_COUNT_MOBILE = 4;
export const ARC_DEPTH_DESKTOP = 7;
export const ARC_DEPTH_MOBILE = 5;

// Waveform
export const VISIBLE_CYCLES_DESKTOP = 3;
export const VISIBLE_CYCLES_MOBILE = 2;
export const LINE_Y_RATIO = 0.40; // Y position as fraction of viewport height

// Sound
export const SOUND_TRIGGER_PROGRESS = 0.70;
export const SOUND_RESET_PROGRESS = 0.30;
export const MASTER_GAIN = 0.12;

// Smooth scroll interpolation
export const SCROLL_LERP_FACTOR = 0.08;

// k2cc wordmark
export const WORDMARK_Y_RATIO = 0.22; // 22vh from top
export const WORDMARK_SIZE_DESKTOP = 120;
export const WORDMARK_SIZE_MOBILE = 60;
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/k2cc-hero/
git commit -m "feat(web): add k2cc pulse animation types and constants"
```

---

### Task 9: Create energy curve module

**Files:**
- Create: `web/src/components/k2cc-hero/energy.ts`

**Context:** This module maps scrollProgress (0→1) to energy parameters. It interpolates between beat boundaries using easeInOutCubic. The 6-beat curve creates the dramatic arc: rest → sense → silence → buildup → burst → aftermath.

- [ ] **Step 1: Create energy.ts**

Create `web/src/components/k2cc-hero/energy.ts`:

```typescript
import type { EnergyParams, BeatName } from './types';
import {
  BEAT_REST_END, BEAT_SENSE_END, BEAT_SILENCE_END,
  BEAT_BUILDUP_END, BEAT_BURST_END,
  COLOR_PRIMARY, COLOR_SILENCE, COLOR_WHITE,
} from './constants';

function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

function lerpColor(a: string, b: string, t: number): string {
  const parseHex = (h: string) => [
    parseInt(h.slice(1, 3), 16),
    parseInt(h.slice(3, 5), 16),
    parseInt(h.slice(5, 7), 16),
  ];
  const [ar, ag, ab] = parseHex(a);
  const [br, bg, bb] = parseHex(b);
  const r = Math.round(lerp(ar, br, t));
  const g = Math.round(lerp(ag, bg, t));
  const bv = Math.round(lerp(ab, bb, t));
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${bv.toString(16).padStart(2, '0')}`;
}

interface BeatDef {
  name: BeatName;
  start: number;
  end: number;
  params: EnergyParams;
}

const BEATS: BeatDef[] = [
  {
    name: 'rest', start: 0, end: BEAT_REST_END,
    params: { amplitude: 30, frequency: 0.8, glowRadius: 15, color: COLOR_PRIMARY, noiseIntensity: 0, lineWidth: 1.5 },
  },
  {
    name: 'sense', start: BEAT_REST_END, end: BEAT_SENSE_END,
    params: { amplitude: 80, frequency: 2.0, glowRadius: 80, color: COLOR_PRIMARY, noiseIntensity: 0.6, lineWidth: 2 },
  },
  {
    name: 'silence', start: BEAT_SENSE_END, end: BEAT_SILENCE_END,
    params: { amplitude: 12, frequency: 0.4, glowRadius: 5, color: COLOR_SILENCE, noiseIntensity: 0, lineWidth: 1 },
  },
  {
    name: 'buildup', start: BEAT_SILENCE_END, end: BEAT_BUILDUP_END,
    params: { amplitude: 150, frequency: 3.5, glowRadius: 200, color: COLOR_WHITE, noiseIntensity: 0.3, lineWidth: 2.5 },
  },
  {
    name: 'burst', start: BEAT_BUILDUP_END, end: BEAT_BURST_END,
    params: { amplitude: 0, frequency: 0, glowRadius: 9999, color: COLOR_WHITE, noiseIntensity: 0, lineWidth: 0 },
  },
  {
    name: 'aftermath', start: BEAT_BURST_END, end: 1.0,
    params: { amplitude: 30, frequency: 0.8, glowRadius: 15, color: COLOR_PRIMARY, noiseIntensity: 0, lineWidth: 1.5 },
  },
];

export function getBeat(progress: number): BeatName {
  for (const beat of BEATS) {
    if (progress < beat.end) return beat.name;
  }
  return 'aftermath';
}

export function getEnergyParams(progress: number): EnergyParams {
  const clampedProgress = Math.max(0, Math.min(1, progress));

  // Find current and next beat
  let currentIdx = 0;
  for (let i = 0; i < BEATS.length; i++) {
    if (clampedProgress < BEATS[i].end) {
      currentIdx = i;
      break;
    }
    if (i === BEATS.length - 1) currentIdx = i;
  }

  const current = BEATS[currentIdx];
  const next = BEATS[Math.min(currentIdx + 1, BEATS.length - 1)];

  // Progress within current beat
  const beatRange = current.end - current.start;
  const beatProgress = beatRange > 0 ? (clampedProgress - current.start) / beatRange : 0;

  // Interpolate toward next beat's params in the second half of current beat
  const blendFactor = beatProgress > 0.5 ? (beatProgress - 0.5) * 2 : 0;
  const blendEased = easeInOutCubic(blendFactor);

  return {
    amplitude: lerp(current.params.amplitude, next.params.amplitude, blendEased),
    frequency: lerp(current.params.frequency, next.params.frequency, blendEased),
    glowRadius: lerp(current.params.glowRadius, next.params.glowRadius, blendEased),
    color: lerpColor(current.params.color, next.params.color, blendEased),
    noiseIntensity: lerp(current.params.noiseIntensity, next.params.noiseIntensity, blendEased),
    lineWidth: lerp(current.params.lineWidth, next.params.lineWidth, blendEased),
  };
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/k2cc-hero/energy.ts
git commit -m "feat(web): add k2cc energy curve — 6-beat scroll-driven parameter interpolation"
```

---

### Task 10: Create waveform module (PQRST + Perlin + glitch)

**Files:**
- Create: `web/src/components/k2cc-hero/waveform.ts`

**Context:** Generates the ECG-like heartbeat waveform. PQRST template from the spec, 1D Perlin noise for distortion during "sense" beat, horizontal glitch tears. All math-driven, no external data.

- [ ] **Step 1: Create waveform.ts**

Create `web/src/components/k2cc-hero/waveform.ts`:

```typescript
import type { GlitchState } from './types';

// Simplified 1D Perlin noise (no library)
function fade(t: number): number { return t * t * t * (t * (t * 6 - 15) + 10); }
function grad(hash: number, x: number): number { return (hash & 1) === 0 ? x : -x; }

const PERM = new Uint8Array(512);
(function initPerm() {
  const p = new Uint8Array(256);
  for (let i = 0; i < 256; i++) p[i] = i;
  // Use a seeded shuffle for deterministic noise across sessions
  let seed = 42;
  const seededRandom = () => {
    seed = (seed * 16807 + 0) % 2147483647;
    return (seed - 1) / 2147483646;
  };
  for (let i = 255; i > 0; i--) {
    const j = Math.floor(seededRandom() * (i + 1));
    [p[i], p[j]] = [p[j], p[i]];
  }
  for (let i = 0; i < 512; i++) PERM[i] = p[i & 255];
})();

export function perlin1d(x: number): number {
  const xi = Math.floor(x) & 255;
  const xf = x - Math.floor(x);
  const u = fade(xf);
  return grad(PERM[xi], xf) * (1 - u) + grad(PERM[xi + 1], xf - 1) * u;
}

// PQRST ECG waveform template
function gaussian(x: number, center: number, width: number): number {
  const d = (x - center) / width;
  return Math.exp(-0.5 * d * d);
}

export function pqrst(t: number): number {
  // Normalized t in [0, 1] per cycle
  const tn = ((t % 1) + 1) % 1;

  if (tn < 0.12) return 0.15 * Math.sin(Math.PI * tn / 0.12);                // P wave
  if (tn < 0.20) return 0;                                                      // PQ segment
  if (tn < 0.24) return -0.1 * gaussian(tn, 0.22, 0.01);                       // Q dip
  if (tn < 0.32) return 1.2 * gaussian(tn, 0.28, 0.015);                       // R spike
  if (tn < 0.36) return -0.15 * gaussian(tn, 0.33, 0.01);                      // S dip
  if (tn < 0.50) return 0;                                                      // ST segment
  if (tn < 0.68) return 0.3 * Math.sin(Math.PI * (tn - 0.50) / 0.18);         // T wave
  return 0;                                                                      // Baseline
}

// R-peak micro-swell: lineWidth boost near R peak
export function rPeakSwell(phase: number): number {
  const smoothstep = (edge0: number, edge1: number, x: number) => {
    const t = Math.max(0, Math.min(1, (x - edge0) / (edge1 - edge0)));
    return t * t * (3 - 2 * t);
  };
  return smoothstep(0.26, 0.28, phase) * smoothstep(0.30, 0.28, phase);
}

// Glitch system state machine
export function updateGlitch(state: GlitchState, scrollProgress: number): GlitchState {
  const inGlitchRange = scrollProgress >= 0.20 && scrollProgress <= 0.65;

  switch (state.phase) {
    case 'idle':
      if (inGlitchRange && Math.random() < 0.003) {
        return {
          phase: 'active',
          framesLeft: 2 + Math.floor(Math.random() * 2),
          cooldownLeft: 0,
          offset: (Math.random() * 2 - 1) * 15,
          width: 80 + Math.random() * 120,
        };
      }
      return state;
    case 'active':
      if (state.framesLeft <= 0) {
        return { ...state, phase: 'cooldown', cooldownLeft: 60 + Math.floor(Math.random() * 60) };
      }
      return { ...state, framesLeft: state.framesLeft - 1 };
    case 'cooldown':
      if (state.cooldownLeft <= 0) {
        return { phase: 'idle', framesLeft: 0, cooldownLeft: 0, offset: 0, width: 0 };
      }
      return { ...state, cooldownLeft: state.cooldownLeft - 1 };
    default:
      return state;
  }
}

// Combined waveform
export function waveform(
  x: number,
  time: number,
  amplitude: number,
  frequency: number,
  noiseIntensity: number,
  viewportWidth: number,
  visibleCycles: number,
): number {
  const wavelength = viewportWidth / visibleCycles;
  const phase = ((x / wavelength + time * frequency) % 1 + 1) % 1;

  let y = pqrst(phase) * amplitude;
  if (noiseIntensity > 0) {
    y += perlin1d(x * 0.01 + time * 2.0) * noiseIntensity * amplitude * 0.5;
  }
  return y;
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/k2cc-hero/waveform.ts
git commit -m "feat(web): add k2cc waveform — PQRST template, Perlin noise, glitch system"
```

---

### Task 11: Create lightning arc module

**Files:**
- Create: `web/src/components/k2cc-hero/lightning.ts`

**Context:** Midpoint displacement algorithm for generating fractal lightning bolts during the "burst" phase. Bolts fire upward from the pulse line toward the k2cc wordmark. Paths regenerated every frame for instability feel.

- [ ] **Step 1: Create lightning.ts**

Create `web/src/components/k2cc-hero/lightning.ts`:

```typescript
import type { BoltSegment } from './types';

interface Point { x: number; y: number; }

function midpoint(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

export function generateBolt(
  a: Point,
  b: Point,
  depth: number,
  displacement: number,
  width: number,
  brightness: number,
): BoltSegment[] {
  if (depth === 0) {
    return [{ x1: a.x, y1: a.y, x2: b.x, y2: b.y, width, brightness, depth: 0 }];
  }

  const mid = midpoint(a, b);
  mid.x += (Math.random() * 2 - 1) * displacement;
  mid.y += (Math.random() * 2 - 1) * displacement;

  const left = generateBolt(a, mid, depth - 1, displacement * 0.55, width, brightness);
  const right = generateBolt(mid, b, depth - 1, displacement * 0.55, width, brightness);

  const segments = [...left, ...right];

  // Branch spawning
  const branchProb = depth >= 5 ? 0.35 : depth >= 3 ? 0.18 : 0.08;
  if (depth > 1 && Math.random() < branchProb) {
    const angle = (Math.atan2(b.y - a.y, b.x - a.x)) + (Math.random() * 60 - 30) * Math.PI / 180;
    const len = Math.sqrt((b.x - a.x) ** 2 + (b.y - a.y) ** 2) * (0.3 + Math.random() * 0.3);
    const branchEnd: Point = {
      x: mid.x + Math.cos(angle) * len,
      y: mid.y + Math.sin(angle) * len,
    };
    const branchSegs = generateBolt(mid, branchEnd, depth - 2, displacement * 0.4, width * 0.6, brightness * 0.7);
    segments.push(...branchSegs);
  }

  return segments;
}

export function generateArcs(
  lineY: number,
  viewportWidth: number,
  wordmarkY: number,
  arcCount: number,
  arcDepth: number,
): BoltSegment[][] {
  const arcs: BoltSegment[][] = [];
  const startX = viewportWidth * 0.3;
  const endX = viewportWidth * 0.7;
  const step = (endX - startX) / Math.max(arcCount - 1, 1);

  for (let i = 0; i < arcCount; i++) {
    const originX = startX + step * i + (Math.random() * 20 - 10);
    const targetX = startX + step * i + (Math.random() * 30 - 15);

    const origin: Point = { x: originX, y: lineY };
    const target: Point = { x: targetX, y: wordmarkY };

    const displacement = 20 + Math.random() * 40;
    const segments = generateBolt(origin, target, arcDepth, displacement, 2, 1);
    arcs.push(segments);
  }

  return arcs;
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/k2cc-hero/lightning.ts
git commit -m "feat(web): add k2cc lightning — midpoint displacement fractal arcs"
```

---

### Task 12: Create particle system module

**Files:**
- Create: `web/src/components/k2cc-hero/particles.ts`

**Context:** Object pool pattern for buildup and aftermath particles. Pre-allocated fixed array, no GC pressure. Buildup particles emit from branch endpoints, aftermath particles from arc shatter points.

- [ ] **Step 1: Create particles.ts**

Create `web/src/components/k2cc-hero/particles.ts`:

```typescript
import type { Particle } from './types';

export function createParticlePool(size: number): Particle[] {
  return Array.from({ length: size }, () => ({
    x: 0, y: 0, vx: 0, vy: 0,
    life: 0, decay: 0, size: 0, brightness: 0,
    active: false,
  }));
}

export function spawnParticle(
  pool: Particle[],
  x: number,
  y: number,
  speed: number,
  size: number,
  decay: number,
): boolean {
  const slot = pool.find((p) => !p.active);
  if (!slot) return false;

  const angle = Math.random() * Math.PI * 2;
  slot.x = x;
  slot.y = y;
  slot.vx = Math.cos(angle) * speed;
  slot.vy = Math.sin(angle) * speed;
  slot.life = 1.0;
  slot.decay = decay;
  slot.size = size;
  slot.brightness = 1.0;
  slot.active = true;
  return true;
}

export function updateParticles(pool: Particle[]): void {
  for (const p of pool) {
    if (!p.active) continue;

    p.x += p.vx;
    p.y += p.vy;
    p.vy += 0.02; // micro-gravity
    p.vx *= 0.98;
    p.vy *= 0.98;
    p.life -= p.decay;
    p.brightness = p.life;

    if (p.life <= 0) {
      p.active = false;
    }
  }
}

function hexToRgb(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `${r},${g},${b}`;
}

export function renderParticles(
  ctx: CanvasRenderingContext2D,
  pool: Particle[],
  color: string,
): void {
  ctx.globalCompositeOperation = 'screen';
  for (const p of pool) {
    if (!p.active) continue;
    const r = p.size * p.life;
    if (r < 0.1) continue;

    ctx.beginPath();
    ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
    ctx.fillStyle = `rgba(${hexToRgb(color)}, ${p.brightness * 0.8})`;
    ctx.fill();
  }
  ctx.globalCompositeOperation = 'source-over';
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/k2cc-hero/particles.ts
git commit -m "feat(web): add k2cc particle system — object pool with physics"
```

---

### Task 13: Create scroll progress hook and audio burst hook

**Files:**
- Create: `web/src/components/k2cc-hero/useScrollProgress.ts`
- Create: `web/src/components/k2cc-hero/useAudioBurst.ts`

- [ ] **Step 1: Create useScrollProgress.ts**

Create `web/src/components/k2cc-hero/useScrollProgress.ts`:

```typescript
import { useRef, useCallback } from 'react';
import { SCROLL_LERP_FACTOR } from './constants';

export function useScrollProgress() {
  const smoothRef = useRef(0);
  const directionRef = useRef<'down' | 'up'>('down');
  const prevRawRef = useRef(0);

  const getProgress = useCallback(() => {
    const scrollY = window.scrollY || window.pageYOffset;
    const maxScroll = document.documentElement.scrollHeight - window.innerHeight;
    const rawProgress = maxScroll > 0 ? scrollY / maxScroll : 0;

    // Track scroll direction
    directionRef.current = rawProgress >= prevRawRef.current ? 'down' : 'up';
    prevRawRef.current = rawProgress;

    // Smooth interpolation
    smoothRef.current += (rawProgress - smoothRef.current) * SCROLL_LERP_FACTOR;

    return {
      raw: rawProgress,
      smooth: smoothRef.current,
      direction: directionRef.current,
    };
  }, []);

  return { getProgress };
}
```

- [ ] **Step 2: Create useAudioBurst.ts**

Create `web/src/components/k2cc-hero/useAudioBurst.ts`:

```typescript
import { useRef, useCallback } from 'react';
import { MASTER_GAIN } from './constants';

export function useAudioBurst() {
  const ctxRef = useRef<AudioContext | null>(null);

  const ensureContext = useCallback(() => {
    if (typeof window === 'undefined') return null;
    // Respect reduced motion preference
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return null;

    if (!ctxRef.current) {
      ctxRef.current = new AudioContext();
    }
    if (ctxRef.current.state === 'suspended') {
      ctxRef.current.resume();
    }
    return ctxRef.current;
  }, []);

  const play = useCallback(() => {
    const ctx = ensureContext();
    if (!ctx) return;

    const master = ctx.createGain();
    master.gain.value = MASTER_GAIN;
    master.connect(ctx.destination);
    const now = ctx.currentTime;

    // Layer 1: Attack — white noise burst 20ms
    const bufferSize = Math.floor(ctx.sampleRate * 0.02);
    const buffer = ctx.createBuffer(1, bufferSize, ctx.sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < bufferSize; i++) data[i] = Math.random() * 2 - 1;
    const noise = ctx.createBufferSource();
    noise.buffer = buffer;
    const hpf = ctx.createBiquadFilter();
    hpf.type = 'highpass';
    hpf.frequency.value = 2000;
    const noiseGain = ctx.createGain();
    noiseGain.gain.setValueAtTime(0, now);
    noiseGain.gain.linearRampToValueAtTime(0.6, now + 0.002);
    noiseGain.gain.linearRampToValueAtTime(0, now + 0.02);
    noise.connect(hpf).connect(noiseGain).connect(master);
    noise.start(now);
    noise.stop(now + 0.02);

    // Layer 2: Body — 60Hz + 120Hz sine
    const body60 = ctx.createOscillator();
    body60.frequency.value = 60;
    const body120 = ctx.createOscillator();
    body120.frequency.value = 120;
    const bodyGain = ctx.createGain();
    bodyGain.gain.setValueAtTime(0.3, now);
    bodyGain.gain.linearRampToValueAtTime(0.2, now + 0.2);
    bodyGain.gain.linearRampToValueAtTime(0, now + 0.5);
    const body120Gain = ctx.createGain();
    body120Gain.gain.value = 0.4;
    body60.connect(bodyGain).connect(master);
    body120.connect(body120Gain).connect(bodyGain);
    body60.start(now);
    body120.start(now);
    body60.stop(now + 0.5);
    body120.stop(now + 0.5);

    // Layer 3: Crackle — 3-5 micro square pulses
    const crackleCount = 3 + Math.floor(Math.random() * 3);
    for (let i = 0; i < crackleCount; i++) {
      const delay = 0.05 + Math.random() * 0.2;
      const dur = 0.005 + Math.random() * 0.01;
      const freq = 1000 + Math.random() * 3000;
      const osc = ctx.createOscillator();
      osc.type = 'square';
      osc.frequency.value = freq;
      const g = ctx.createGain();
      g.gain.setValueAtTime(0.15 + Math.random() * 0.1, now + delay);
      g.gain.linearRampToValueAtTime(0, now + delay + dur);
      osc.connect(g).connect(master);
      osc.start(now + delay);
      osc.stop(now + delay + dur);
    }

    // Layer 4: Sub bass — 35Hz
    const sub = ctx.createOscillator();
    sub.frequency.value = 35;
    const subGain = ctx.createGain();
    subGain.gain.setValueAtTime(0.4, now);
    subGain.gain.linearRampToValueAtTime(0.2, now + 0.1);
    subGain.gain.linearRampToValueAtTime(0, now + 0.4);
    sub.connect(subGain).connect(master);
    sub.start(now);
    sub.stop(now + 0.4);
  }, [ensureContext]);

  return { play };
}
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/k2cc-hero/useScrollProgress.ts web/src/components/k2cc-hero/useAudioBurst.ts
git commit -m "feat(web): add k2cc scroll progress hook and audio burst hook"
```

---

### Task 14: Create main renderer

**Files:**
- Create: `web/src/components/k2cc-hero/renderer.ts`

**Context:** The core render loop. Called every frame via requestAnimationFrame. Computes energy params from scroll progress, draws glow layer, pulse line, branches, lightning arcs, particles, and k2cc wordmark. React-independent — takes a canvas context and state.

- [ ] **Step 1: Create renderer.ts**

Create `web/src/components/k2cc-hero/renderer.ts`:

```typescript
import type { RenderState } from './types';
import { getEnergyParams, getBeat } from './energy';
import { waveform, updateGlitch } from './waveform';
import { generateArcs } from './lightning';
import { updateParticles, renderParticles, spawnParticle } from './particles';
import type { Particle } from './types';
import {
  COLOR_PRIMARY, COLOR_WHITE,
  LINE_Y_RATIO, VISIBLE_CYCLES_DESKTOP, VISIBLE_CYCLES_MOBILE,
  ARC_COUNT_DESKTOP, ARC_COUNT_MOBILE, ARC_DEPTH_DESKTOP, ARC_DEPTH_MOBILE,
  WORDMARK_Y_RATIO, SOUND_TRIGGER_PROGRESS, SOUND_RESET_PROGRESS,
} from './constants';

function hexToRgba(hex: string, alpha: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

export interface TickContext {
  ctx: CanvasRenderingContext2D;
  width: number;
  height: number;
  isMobile: boolean;
  particles: Particle[];
  playSound: () => void;
}

export function tick(
  tickCtx: TickContext,
  state: RenderState,
  deltaTime: number,
): RenderState {
  const { ctx, width, height, isMobile, particles, playSound } = tickCtx;
  const { smoothProgress, time } = state;

  // 1. Energy params
  const energy = getEnergyParams(smoothProgress);
  const beat = getBeat(smoothProgress);

  // 2. Clear at identity (avoid shake ghosting)
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  ctx.restore();

  const lineY = height * LINE_Y_RATIO;
  const visibleCycles = isMobile ? VISIBLE_CYCLES_MOBILE : VISIBLE_CYCLES_DESKTOP;

  // 3. Screen shake
  let shakeX = 0, shakeY = 0;
  if (beat === 'burst') {
    shakeX = (Math.random() * 2 - 1) * 2;
    shakeY = (Math.random() * 2 - 1) * 2;
  } else if (beat === 'buildup' && smoothProgress > 0.55) {
    shakeX = (Math.random() * 2 - 1) * 1;
    shakeY = (Math.random() * 2 - 1) * 1;
  }
  ctx.save();
  ctx.translate(shakeX, shakeY);

  // 4. Glow layer
  if (beat !== 'burst') {
    ctx.globalCompositeOperation = 'screen';
    const intensity = beat === 'silence' ? 0.02 : beat === 'buildup' ? 0.15 : 0.03;
    const glowR = Math.min(energy.glowRadius, Math.max(width, height));
    const gradient = ctx.createRadialGradient(width / 2, lineY, 0, width / 2, lineY, glowR);
    gradient.addColorStop(0, hexToRgba(energy.color, intensity));
    gradient.addColorStop(0.4, hexToRgba(energy.color, intensity * 0.3));
    gradient.addColorStop(1, hexToRgba(energy.color, 0));
    ctx.fillStyle = gradient;
    ctx.fillRect(width / 2 - glowR, lineY - glowR, glowR * 2, glowR * 2);
    ctx.globalCompositeOperation = 'source-over';
  } else {
    // Burst full-screen flash
    ctx.globalCompositeOperation = 'screen';
    ctx.fillStyle = hexToRgba(COLOR_PRIMARY, 0.05 + Math.random() * 0.1);
    ctx.fillRect(0, 0, width, height);
    ctx.globalCompositeOperation = 'source-over';
  }

  // 5. Main pulse line (not during burst)
  if (beat !== 'burst') {
    const glitch = updateGlitch(state.glitch, smoothProgress);

    ctx.beginPath();
    ctx.strokeStyle = energy.color;
    ctx.lineWidth = energy.lineWidth;
    ctx.shadowColor = energy.color;
    ctx.shadowBlur = 4;

    for (let x = 0; x <= width; x++) {
      let drawX = x;
      // Apply glitch displacement
      if (glitch.phase === 'active' && Math.abs(x - width / 2) < glitch.width / 2) {
        drawX += glitch.offset;
      }
      const y = lineY + waveform(x, time, energy.amplitude, energy.frequency, energy.noiseIntensity, width, visibleCycles);

      if (x === 0) ctx.moveTo(drawX, y);
      else ctx.lineTo(drawX, y);
    }
    ctx.stroke();
    ctx.shadowBlur = 0;

    // Buildup: spawn particles from wave peaks
    if (beat === 'buildup' && Math.random() < 0.1) {
      const peakX = Math.random() * width;
      const peakY = lineY + waveform(peakX, time, energy.amplitude, energy.frequency, energy.noiseIntensity, width, visibleCycles);
      spawnParticle(particles, peakX, peakY, 1 + Math.random() * 2, 1 + Math.random(), 0.02 + Math.random() * 0.02);
    }

    // Update and render particles
    updateParticles(particles);
    renderParticles(ctx, particles, COLOR_PRIMARY);

    ctx.restore();

    return {
      ...state,
      time: time + deltaTime * 0.001,
      beat,
      energy,
      glitch,
      shakeX,
      shakeY,
    };
  }

  // 6. Lightning arcs (burst phase)
  const wordmarkY = height * WORDMARK_Y_RATIO;
  const arcCount = isMobile ? ARC_COUNT_MOBILE : ARC_COUNT_DESKTOP;
  const arcDepth = isMobile ? ARC_DEPTH_MOBILE : ARC_DEPTH_DESKTOP;
  const arcs = generateArcs(lineY, width, wordmarkY, arcCount, arcDepth);

  for (const arc of arcs) {
    for (const seg of arc) {
      ctx.beginPath();
      ctx.moveTo(seg.x1, seg.y1);
      ctx.lineTo(seg.x2, seg.y2);
      ctx.strokeStyle = hexToRgba(COLOR_WHITE, seg.brightness * (0.7 + Math.random() * 0.3));
      ctx.lineWidth = seg.width;
      ctx.shadowColor = COLOR_PRIMARY;
      ctx.shadowBlur = 6;
      ctx.stroke();
    }
  }
  ctx.shadowBlur = 0;

  // k2cc wordmark during burst
  const burstProgress = (smoothProgress - 0.65) / 0.15;
  let wordmarkOpacity = Math.min(1, Math.max(0, burstProgress));

  // Aftermath: wordmark fade out
  if (smoothProgress > 0.93) {
    wordmarkOpacity = Math.max(0, 1 - (smoothProgress - 0.93) / 0.03);
  }

  if (wordmarkOpacity > 0) {
    ctx.font = `bold ${isMobile ? 60 : 120}px monospace`;
    ctx.textAlign = 'center';
    ctx.fillStyle = hexToRgba(COLOR_WHITE, wordmarkOpacity);
    ctx.shadowColor = COLOR_PRIMARY;
    ctx.shadowBlur = 6;
    ctx.fillText('k2cc', width / 2, wordmarkY + (isMobile ? 20 : 40));
    ctx.shadowBlur = 0;
  }

  // Aftermath particles from arc endpoints
  if (smoothProgress > 0.80 && Math.random() < 0.3) {
    const arcSet = arcs[Math.floor(Math.random() * arcs.length)];
    if (arcSet && arcSet.length > 0) {
      const seg = arcSet[Math.floor(Math.random() * arcSet.length)];
      for (let i = 0; i < 3; i++) {
        spawnParticle(particles, seg.x2, seg.y2, 2 + Math.random() * 3, 2 + Math.random(), 0.015 + Math.random() * 0.025);
      }
    }
  }

  // 7. Sound trigger — only when scrolling DOWN past threshold (per spec)
  let hasPlayedSound = state.hasPlayedSound;
  if (smoothProgress >= SOUND_TRIGGER_PROGRESS && !hasPlayedSound && state.scrollDirection === 'down') {
    playSound();
    hasPlayedSound = true;
  }
  if (smoothProgress < SOUND_RESET_PROGRESS) {
    hasPlayedSound = false;
  }

  // 8. Update and render particles
  updateParticles(particles);
  renderParticles(ctx, particles, beat === 'burst' || beat === 'aftermath' ? COLOR_WHITE : COLOR_PRIMARY);

  ctx.restore();

  return {
    ...state,
    time: time + deltaTime * 0.001,
    beat,
    energy,
    shakeX,
    shakeY,
    hasPlayedSound,
    wordmarkOpacity,
  };
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/k2cc-hero/renderer.ts
git commit -m "feat(web): add k2cc main renderer — glow, waveform, lightning, particles"
```

---

### Task 15: Create K2ccPulseCanvas component and integrate into homepage

**Files:**
- Create: `web/src/components/k2cc-hero/K2ccPulseCanvas.tsx`
- Create: `web/src/app/[locale]/HomeClient.tsx`
- Modify: `web/src/app/[locale]/page.tsx` (add HomeClient import + render)

**Context:** K2ccPulseCanvas is a client component rendering a fixed `<canvas>` at z-index 0. Content sections have `relative z-10` to sit above it. The `prefers-reduced-motion` media query shows only a static green line. HomeClient uses `next/dynamic` with `ssr: false` to avoid server-side Canvas errors.

- [ ] **Step 1: Create K2ccPulseCanvas.tsx**

Create `web/src/components/k2cc-hero/K2ccPulseCanvas.tsx`:

```tsx
'use client';

import { useRef, useEffect, useCallback } from 'react';
import { useScrollProgress } from './useScrollProgress';
import { useAudioBurst } from './useAudioBurst';
import { tick, type TickContext } from './renderer';
import { createParticlePool } from './particles';
import { getEnergyParams } from './energy';
import type { RenderState } from './types';
import { PARTICLE_POOL_DESKTOP, PARTICLE_POOL_MOBILE, MAX_DPR_DESKTOP } from './constants';

export default function K2ccPulseCanvas() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rafRef = useRef<number>(0);
  const stateRef = useRef<RenderState | null>(null);
  const particlesRef = useRef(createParticlePool(PARTICLE_POOL_DESKTOP));
  const { getProgress } = useScrollProgress();
  const { play: playSound } = useAudioBurst();
  const lastTimeRef = useRef(0);

  const isMobileRef = useRef(false);
  const reducedMotionRef = useRef(false);

  const setupCanvas = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return null;

    const rect = canvas.getBoundingClientRect();
    const dpr = Math.min(window.devicePixelRatio, MAX_DPR_DESKTOP);
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;

    const ctx = canvas.getContext('2d');
    if (!ctx) return null;
    ctx.scale(dpr, dpr);

    isMobileRef.current = rect.width < 768;
    if (isMobileRef.current) {
      particlesRef.current = createParticlePool(PARTICLE_POOL_MOBILE);
    }

    return { ctx, width: rect.width, height: rect.height };
  }, []);

  useEffect(() => {
    // Check reduced motion
    reducedMotionRef.current = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    const setup = setupCanvas();
    if (!setup) return;

    // Initialize state
    stateRef.current = {
      scrollProgress: 0,
      smoothProgress: 0,
      scrollDirection: 'down',
      time: 0,
      beat: 'rest',
      energy: getEnergyParams(0),
      glitch: { phase: 'idle', framesLeft: 0, cooldownLeft: 0, offset: 0, width: 0 },
      particles: particlesRef.current,
      shakeX: 0,
      shakeY: 0,
      hasPlayedSound: false,
      wordmarkOpacity: 0,
    };

    // Resize observer (debounced)
    let resizeTimeout: ReturnType<typeof setTimeout>;
    const observer = new ResizeObserver(() => {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => setupCanvas(), 100);
    });
    if (canvasRef.current) observer.observe(canvasRef.current);

    // Visibility check
    let visible = true;
    const onVisibility = () => {
      visible = !document.hidden;
      if (visible) lastTimeRef.current = 0; // reset delta
    };
    document.addEventListener('visibilitychange', onVisibility);

    // Animation loop
    const loop = (timestamp: number) => {
      if (!visible || !stateRef.current || !canvasRef.current) {
        rafRef.current = requestAnimationFrame(loop);
        return;
      }

      const deltaTime = lastTimeRef.current ? timestamp - lastTimeRef.current : 16;
      lastTimeRef.current = timestamp;

      const progress = getProgress();
      stateRef.current.scrollProgress = progress.raw;
      stateRef.current.smoothProgress = progress.smooth;
      stateRef.current.scrollDirection = progress.direction;

      const canvas = canvasRef.current;
      const ctx = canvas.getContext('2d');
      if (!ctx) {
        rafRef.current = requestAnimationFrame(loop);
        return;
      }

      const rect = canvas.getBoundingClientRect();

      if (reducedMotionRef.current) {
        // Static mode: single green line
        ctx.save();
        ctx.setTransform(1, 0, 0, 1, 0, 0);
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.restore();
        ctx.strokeStyle = '#00ff88';
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        const y = rect.height * 0.4;
        ctx.moveTo(0, y);
        ctx.lineTo(rect.width, y);
        ctx.stroke();
      } else {
        const tickCtx: TickContext = {
          ctx,
          width: rect.width,
          height: rect.height,
          isMobile: isMobileRef.current,
          particles: particlesRef.current,
          playSound,
        };

        stateRef.current = tick(tickCtx, stateRef.current, deltaTime);
      }

      rafRef.current = requestAnimationFrame(loop);
    };

    rafRef.current = requestAnimationFrame(loop);

    return () => {
      cancelAnimationFrame(rafRef.current);
      clearTimeout(resizeTimeout);
      observer.disconnect();
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [setupCanvas, getProgress, playSound]);

  return (
    <canvas
      ref={canvasRef}
      className="fixed inset-0 w-full h-full pointer-events-none"
      style={{ zIndex: 0 }}
      aria-hidden="true"
    />
  );
}
```

- [ ] **Step 2: Create HomeClient.tsx**

Create `web/src/app/[locale]/HomeClient.tsx`:

```tsx
'use client';

import dynamic from 'next/dynamic';

const K2ccPulseCanvas = dynamic(
  () => import('@/components/k2cc-hero/K2ccPulseCanvas'),
  { ssr: false },
);

export default function HomeClient() {
  return <K2ccPulseCanvas />;
}
```

- [ ] **Step 3: Update page.tsx — add HomeClient**

In `web/src/app/[locale]/page.tsx`, add import:

```tsx
import HomeClient from './HomeClient';
```

And add `<HomeClient />` right after `<Header />`:

```tsx
<Header />
<HomeClient />
```

- [ ] **Step 4: Verify build and canvas rendering**

Run: `cd web && npx next build 2>&1 | tail -20`

Then `cd web && yarn dev` and open `http://localhost:3000/zh-CN`:
- Fixed canvas should render behind all content
- Green ECG heartbeat line at ~40% viewport height
- Scrolling should change the waveform energy
- Content sections should be readable over the canvas

- [ ] **Step 5: Commit**

```bash
git add web/src/components/k2cc-hero/K2ccPulseCanvas.tsx web/src/app/[locale]/HomeClient.tsx web/src/app/[locale]/page.tsx
git commit -m "feat(web): integrate k2cc pulse Canvas — fixed background with scroll-driven animation"
```

---

### Task 16: Final verification and cleanup

- [ ] **Step 1: Run TypeScript type check**

Run: `cd web && npx tsc --noEmit 2>&1 | head -30`

Fix any type errors.

- [ ] **Step 2: Run linter**

Run: `cd web && yarn lint 2>&1 | tail -20`

Fix any lint issues.

- [ ] **Step 3: Run tests**

Run: `cd web && yarn test 2>&1 | tail -30`

All existing tests should still pass.

- [ ] **Step 4: Cross-locale verification**

Open each locale in the browser and verify hero text, CTA buttons, feature titles, and download CTA:
- `http://localhost:3000/zh-CN`
- `http://localhost:3000/en-US`
- `http://localhost:3000/ja`
- `http://localhost:3000/zh-TW`

- [ ] **Step 5: Performance check — Canvas frame time**

Open Chrome DevTools → Performance tab → Record while scrolling through the page slowly. Check:
- Frame time under 16ms (60fps target)
- No memory leaks (stable JS heap)
- Canvas rendering not blocking main thread

- [ ] **Step 6: Commit all remaining changes**

```bash
git add -A
git commit -m "feat(web): homepage optimization complete — cleanup and verification"
```

---

## Summary of Changes

| Area | Before | After |
|------|--------|-------|
| page.tsx | 553 lines, monolithic | ~100 lines, thin compositor |
| Components | None | HeroSection, FeaturesSection, DownloadCTA |
| Styling | Mixed inline `style={{}}` + Tailwind | Tailwind-only |
| Feature icons | 6 emoji | 6 Lucide icons |
| Comparison table | 9-column protocol matrix | Removed (available on /k2 page) |
| Download section | 4 platform cards with CDN links | Compact CTA linking to /install |
| CTA primary | "开通和续费" | "我要买" |
| Header nav | K2 Protocol, Routers, GitHub | + Quick Start, Download button |
| Background | Static #0a0a0f | k2cc pulse Canvas animation |
| Platforms listed | 4 (iOS/Android/Windows/macOS) | 5 (+ Linux) |
