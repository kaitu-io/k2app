// Mirrors routing.locales in `src/i18n/routing.ts`. Kept inline to avoid pulling
// `next-intl/navigation` (and transitively `next/navigation`) into vitest's module
// graph when non-component code imports brand config.
export const ALL_LOCALES = ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'] as const;
type Locale = (typeof ALL_LOCALES)[number];

export type BrandId = 'kaitu' | 'overleap';

export type Brand = {
  id: BrandId;
  displayName: string;
  wordmark: string;
  legalName: string;
  baseUrl: string;
  defaultLocale: Locale;
  allowedLocales: readonly Locale[];
  logoPath: string;
  contactEmail: string;
  ogImagePath: string;
  taglineZh?: string;
  /** User-facing product badge, e.g. gift-code redemption pages. */
  productName: string;
  /** Prefix under /public for the favicon set. '' = legacy root files (kaitu). */
  faviconPrefix: string;
  /** Google Analytics measurement id. '' = GA disabled for this brand. */
  gaMeasurementId: string;
  /** Chatwoot website token. '' = support widget disabled for this brand. */
  chatwootToken: string;
  /**
   * Onboarding guide video (Support page player + its VideoObject JSON-LD).
   * '' = this brand has no guide video: the player and the JSON-LD block are
   * both omitted rather than falling back to another brand's asset.
   */
  guideVideoUrl: string;
  /** Download CDN layout (spec §8: /kaitu/ vs /overleap/ path segments, Kaitu_* vs Overleap_* artifacts). */
  cdn: {
    desktopBases: readonly string[];
    mobileBases: readonly string[];
    artifactPrefix: string;
  };
  /** Brand feature gates for web surfaces. */
  features: {
    routers: boolean;
    linuxInstall: boolean;
    androidApkGuide: boolean;
  };
};

export const KAITU: Brand = {
  id: 'kaitu',
  displayName: 'Kaitu',
  wordmark: '开途',
  // Both brands are operated by the same legal entity; legal documents on BOTH
  // deployments sign "Overleap LLC". This is the ONLY approved cross-brand
  // appearance (root CLAUDE.md: 法务文书署名 Overleap LLC 除外) and is scoped
  // to legal-signature surfaces by tests/brand-guard.test.ts.
  legalName: 'Overleap LLC',
  baseUrl: 'https://kaitu.io',
  defaultLocale: 'zh-CN',
  allowedLocales: ['zh-CN', 'zh-TW', 'zh-HK'],
  logoPath: '/kaitu-icon.png',
  contactEmail: 'support@kaitu.me',
  ogImagePath: '/images/og-default.png',
  taglineZh: '愿上帝为你开路',
  productName: '开途 VPN',
  faviconPrefix: '',
  gaMeasurementId: 'G-EH2PY4S0CX',
  chatwootToken: 'ZfFNvQRuoKzkik6X4KCSgp1h',
  guideVideoUrl: 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/kaitu_guide.mp4',
  cdn: {
    desktopBases: [
      'https://dl.kaitu.io/kaitu/desktop',
      'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop',
    ],
    mobileBases: [
      'https://dl.kaitu.io/kaitu',
      'https://d13jc1jqzlg4yt.cloudfront.net/kaitu',
    ],
    artifactPrefix: 'Kaitu',
  },
  features: { routers: true, linuxInstall: true, androidApkGuide: true },
};

export const OVERLEAP: Brand = {
  id: 'overleap',
  displayName: 'Overleap',
  wordmark: 'Overleap',
  legalName: 'Overleap LLC',
  baseUrl: 'https://overleap.io',
  defaultLocale: 'en-US',
  allowedLocales: ['en-US', 'en-GB', 'en-AU', 'ja'],
  logoPath: '/overleap-icon.png',
  contactEmail: 'support@overleap.io',
  ogImagePath: '/overleap-og.png',
  productName: 'Overleap',
  faviconPrefix: '/brand/overleap',
  gaMeasurementId: '',   // Open Question #1: create GA4 property, then fill in
  chatwootToken: '',     // Open Question #1: create Chatwoot inbox, then fill in
  // No Overleap-branded guide video has been produced yet. Empty on purpose:
  // the Support page omits the player + VideoObject rather than serving the
  // 开途-branded recording (spec: overleap 站 0 处 kaitu).
  guideVideoUrl: '',
  cdn: {
    // /overleap/ CDN path per spec §8. dl.overleap.io CNAME does not exist yet
    // (Open Question #2) — raw CloudFront until provisioned. Artifacts appear in Phase 4/5.
    desktopBases: ['https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop'],
    mobileBases: ['https://d13jc1jqzlg4yt.cloudfront.net/overleap'],
    artifactPrefix: 'Overleap',
  },
  features: { routers: false, linuxInstall: false, androidApkGuide: false },
};

export function brandById(id: BrandId): Brand {
  return id === 'overleap' ? OVERLEAP : KAITU;
}

/**
 * Build-time brand id parsing. Exact-match on purpose: NEXT_PUBLIC_BRAND is a
 * build variable we control, and any drift (typo, unset) must fall back to
 * kaitu — the zero-breakage default, mirroring api/brand.go resolveRequestBrand.
 */
export function parseBrandId(raw: string | undefined | null): BrandId {
  return raw === 'overleap' ? 'overleap' : 'kaitu';
}

/**
 * The baked deployment brand. Single source of truth for Phase 2's
 * one-codebase-two-deployments model: NEXT_PUBLIC_BRAND is set per Amplify app
 * and inlined into client bundles at build time; server/middleware read it from
 * the environment baked into .env.production by amplify.yml.
 */
export function siteBrand(): Brand {
  return brandById(parseBrandId(process.env.NEXT_PUBLIC_BRAND));
}
