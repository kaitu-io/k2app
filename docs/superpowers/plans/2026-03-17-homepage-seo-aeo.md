# Homepage SEO & AEO Optimization Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix homepage metadata to inherit layout's full SEO infrastructure (OG image, canonical, hreflang) while keeping the brand slogan title; regenerate OG image with correct branding; add Organization + FAQPage JSON-LD for AEO.

**Architecture:** Homepage `generateMetadata()` calls layout's `generateMetadata()` then overrides title/description, syncing across `<title>`, `og:title`, and `twitter:title`. OG image regenerated with brand slogan. JSON-LD expanded to 3 schema blocks.

**Tech Stack:** Next.js 15 Metadata API, Schema.org JSON-LD, Node.js Canvas (OG image generation)

---

## Brand Hierarchy

| Element | zh-CN | en-US |
|---------|-------|-------|
| Title (h1 + `<title>`) | 别人断线，你满速。 | Others Drop. You Don't. |
| Subtitle/Tagline | 越拥堵，越从容。 | Thrives where networks struggle. |
| SEO Description | k2cc 重写拥塞控制规则… | k2cc rewrites congestion control… |

---

## Task 1: Fix homepage generateMetadata — inherit layout + override title

**Files:**
- Modify: `web/src/app/[locale]/page.tsx`

**Context:** Layout's `metadata.ts` exports `generateMetadata(locale, pathname, overrides)` which returns full metadata. The homepage should call it, then override title/description to use the brand slogan. This preserves OG image, canonical URL, hreflang alternates, and favicons from the layout.

- [ ] **Step 1: Rewrite generateMetadata in page.tsx**

Replace the current `generateMetadata` function with one that calls the layout's metadata function:

```tsx
import { generateMetadata as generateBaseMetadata } from './metadata';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const base = generateBaseMetadata(locale);
  const t = await getTranslations({ locale, namespace: 'hero' });

  const title = `${t('hero.title')} | Kaitu k2`;
  const description = t('hero.description');

  return {
    ...base,
    title,
    description,
    openGraph: {
      ...(base.openGraph as Record<string, unknown>),
      title,
      description,
    },
    twitter: {
      ...(base.twitter as Record<string, unknown>),
      title,
      description,
    },
  };
}
```

Keep the `Metadata` type import.

- [ ] **Step 2: Run tests**

Run: `cd web && npx vitest run tests/homepage-ssr.test.ts`

- [ ] **Step 3: Commit**

```bash
git add src/app/[locale]/page.tsx
git commit -m "fix(seo): homepage inherits layout OG/canonical/hreflang, overrides title with brand slogan"
```

---

## Task 2: Add Organization + FAQPage JSON-LD

**Files:**
- Modify: `web/src/app/[locale]/page.tsx`

- [ ] **Step 1: Replace JSON_LD_CONTENT with 3-schema array**

Replace the `JSON_LD_CONTENT` constant with an array containing SoftwareApplication + Organization + FAQPage. The FAQPage targets real AI search queries like "What is k2cc?", "How does ECH work?", "k2 vs Hysteria2".

```tsx
const JSON_LD_CONTENT = JSON.stringify([
  {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: 'Kaitu k2',
    applicationCategory: 'NetworkingApplication',
    operatingSystem: 'Windows, macOS, iOS, Android, Linux',
    description:
      'ECH-based stealth tunnel protocol powered by k2cc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
    url: 'https://kaitu.io',
    publisher: { '@type': 'Organization', name: 'Kaitu', url: 'https://kaitu.io' },
    offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
    featureList: [
      'ECH (Encrypted Client Hello) stealth',
      'QUIC + TCP-WebSocket dual-stack transport',
      'k2cc adaptive rate control',
      'Reverse proxy camouflage',
      'Self-signed certificate + certificate pinning',
      'Zero CT log exposure',
      'One-command deployment',
    ],
  },
  {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: 'Kaitu',
    url: 'https://kaitu.io',
    logo: 'https://kaitu.io/kaitu-icon.png',
    sameAs: ['https://github.com/kaitu-io'],
    contactPoint: { '@type': 'ContactPoint', email: 'support@kaitu.me', contactType: 'customer support' },
  },
  {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: [
      {
        '@type': 'Question',
        name: 'What is k2cc congestion control?',
        acceptedAnswer: {
          '@type': 'Answer',
          text: 'k2cc is an adaptive rate control algorithm that distinguishes censorship-induced packet drops from normal congestion. It maintains stable throughput under QoS throttling and high packet loss conditions where traditional algorithms like BBR degrade significantly.',
        },
      },
      {
        '@type': 'Question',
        name: 'What is ECH and how does k2 use it?',
        acceptedAnswer: {
          '@type': 'Answer',
          text: 'ECH (Encrypted Client Hello) encrypts the SNI field in TLS handshakes, making it impossible for deep packet inspection to identify the destination. k2v5 uses ECH as its primary stealth mechanism, combined with reverse proxy camouflage for active probe defense.',
        },
      },
      {
        '@type': 'Question',
        name: 'How do I install k2 server?',
        acceptedAnswer: {
          '@type': 'Answer',
          text: 'Run: curl -fsSL https://kaitu.io/i/k2s | sudo sh — this installs the k2s server and auto-generates a k2v5:// connection URI. No manual certificate configuration needed.',
        },
      },
      {
        '@type': 'Question',
        name: 'What platforms does Kaitu k2 support?',
        acceptedAnswer: {
          '@type': 'Answer',
          text: 'Kaitu k2 supports Windows 10/11, macOS 12+, Linux (AppImage), iOS (iPhone/iPad), and Android. Desktop clients are available for direct download, mobile apps via App Store and APK.',
        },
      },
      {
        '@type': 'Question',
        name: 'How does k2 compare to VLESS+Reality and Hysteria2?',
        acceptedAnswer: {
          '@type': 'Answer',
          text: 'k2v5 is the only protocol combining ECH stealth, QUIC+TCP-WS dual-stack, adaptive congestion control (k2cc), zero CT log exposure, and one-command deployment. VLESS+Reality lacks ECH and congestion control. Hysteria2 has QUIC and congestion control but no ECH stealth or TCP fallback.',
        },
      },
    ],
  },
]);
```

- [ ] **Step 2: Commit**

```bash
git add src/app/[locale]/page.tsx
git commit -m "feat(aeo): add Organization + FAQPage JSON-LD for AI search optimization"
```

---

## Task 3: Regenerate og-default.png with brand slogan

**Files:**
- Create: `web/scripts/generate-og-image.mjs` (one-time script)
- Replace: `web/public/images/og-default.png`

**Context:** Current OG image shows "30% Packet Loss. Full Speed." in English. Should match the brand: main slogan in both languages, using the homepage color scheme (#050508 bg, #00ff88 primary, #ffffff text).

Design:
- 1200×630 px (OG standard)
- Background: #050508
- Main text: "别人断线，你满速。" — bold, white, large
- Secondary: "Others Drop. You Don't." — #00ff88, medium
- Tagline: "k2cc — 越拥堵，越从容" — muted gray, small
- Bottom: "kaitu.io" — muted, small
- Subtle green glow line at ~60% height (echoing the pulse animation)

- [ ] **Step 1: Install canvas dependency (dev only)**

Run: `cd web && yarn add -D @napi-rs/canvas`

- [ ] **Step 2: Create generation script**

Create `web/scripts/generate-og-image.mjs`:

```javascript
import { createCanvas } from '@napi-rs/canvas';
import { writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));

const WIDTH = 1200;
const HEIGHT = 630;

const canvas = createCanvas(WIDTH, HEIGHT);
const ctx = canvas.getContext('2d');

// Background
ctx.fillStyle = '#050508';
ctx.fillRect(0, 0, WIDTH, HEIGHT);

// Subtle green glow line at 60% height
const glowY = HEIGHT * 0.6;
const gradient = ctx.createRadialGradient(WIDTH / 2, glowY, 0, WIDTH / 2, glowY, 300);
gradient.addColorStop(0, 'rgba(0, 255, 136, 0.08)');
gradient.addColorStop(1, 'rgba(0, 255, 136, 0)');
ctx.fillStyle = gradient;
ctx.fillRect(0, glowY - 300, WIDTH, 600);

// Thin green pulse line
ctx.strokeStyle = 'rgba(0, 255, 136, 0.3)';
ctx.lineWidth = 1.5;
ctx.beginPath();
ctx.moveTo(0, glowY);
ctx.lineTo(WIDTH, glowY);
ctx.stroke();

// Main title — Chinese slogan
ctx.fillStyle = '#ffffff';
ctx.font = 'bold 72px "PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif';
ctx.textAlign = 'center';
ctx.textBaseline = 'middle';
ctx.fillText('别人断线，你满速。', WIDTH / 2, HEIGHT * 0.30);

// Secondary — English slogan
ctx.fillStyle = '#00ff88';
ctx.font = 'bold 40px "SF Mono", "JetBrains Mono", "Fira Code", monospace';
ctx.fillText('Others Drop. You Don\'t.', WIDTH / 2, HEIGHT * 0.44);

// Tagline
ctx.fillStyle = '#9ca3af';
ctx.font = '24px "PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif';
ctx.fillText('k2cc — 越拥堵，越从容', WIDTH / 2, HEIGHT * 0.75);

// Domain
ctx.fillStyle = '#6b7280';
ctx.font = '20px "SF Mono", "JetBrains Mono", monospace';
ctx.fillText('kaitu.io', WIDTH / 2, HEIGHT * 0.88);

// Save
const buffer = canvas.toBuffer('image/png');
const outputPath = resolve(__dirname, '../public/images/og-default.png');
writeFileSync(outputPath, buffer);
console.log(`OG image generated: ${outputPath} (${buffer.length} bytes)`);
```

- [ ] **Step 3: Run the script**

Run: `cd web && node scripts/generate-og-image.mjs`

Expected: "OG image generated: .../og-default.png (XXXXX bytes)"

- [ ] **Step 4: Verify the generated image**

View the image to confirm it looks correct: text readable, colors match, 1200×630.

- [ ] **Step 5: Remove dev dependency (optional — script is one-time)**

Run: `cd web && yarn remove @napi-rs/canvas`

(Keep the script in `scripts/` for future regeneration.)

- [ ] **Step 6: Commit**

```bash
git add public/images/og-default.png scripts/generate-og-image.mjs
git commit -m "feat(seo): regenerate og-default.png with brand slogan"
```

---

## Task 4: Verify

- [ ] **Step 1: Run tests**

Run: `cd web && npx vitest run tests/homepage-ssr.test.ts`

- [ ] **Step 2: View page source in dev server**

Open `http://localhost:3001/zh-CN`, view source:
1. `<title>` contains "别人断线，你满速。"
2. `og:title` matches `<title>`
3. `og:image` points to `/images/og-default.png`
4. `canonical` is `https://kaitu.io/zh-CN`
5. `hreflang` alternates for all 7 locales
6. 3 JSON-LD blocks in page source

---

## Summary

| Area | Before | After |
|------|--------|-------|
| `<title>` | `"别人断线，你满速。 Kaitu k2"` | `"别人断线，你满速。 \| Kaitu k2"` |
| og:title | Missing (layout override wiped) | Synced with `<title>` |
| OG image | English "30% Packet Loss" | Bilingual brand slogan |
| Canonical + hreflang | Missing from page | Inherited from layout |
| JSON-LD | 1 block | 3 blocks (+Organization, +FAQPage) |
| AEO queries | 0 | 5 FAQ entries (k2cc, ECH, install, platforms, comparison) |
