// Mirrors routing.locales in `src/i18n/routing.ts`. Kept inline to avoid pulling
// `next-intl/navigation` (and transitively `next/navigation`) into vitest's module
// graph when non-component code imports brand config.
export const ALL_LOCALES = ['zh-CN', 'zh-TW', 'zh-HK'] as const;
type Locale = (typeof ALL_LOCALES)[number];

/**
 * Every brand the backend knows. The site serves only kaitu, but the admin
 * dashboard (/manager) is cross-brand and the cross-layer contract checks the
 * full set against api/brand.go.
 */
export type BrandId = 'kaitu' | 'overleap';

/** What any surface needs to name / link / attribute a brand. */
export type BrandIdentity = {
  id: BrandId;
  displayName: string;
  wordmark: string;
  baseUrl: string;
  contactEmail: string;
};

/** The served brand's full site configuration. */
export type Brand = BrandIdentity & {
  legalName: string;
  defaultLocale: Locale;
  allowedLocales: readonly Locale[];
  logoPath: string;
  /** Recipient named by the privacy policy for data-subject requests. */
  privacyEmail: string;
  /** Recipient named by the terms of service for legal enquiries. */
  legalEmail: string;
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
   * In-house visitor chat widget (pre-purchase pages). false = the widget never
   * renders and never calls /api/chat for this brand; true still needs the
   * server switch (`session.enabled`) before anything shows.
   */
  chatEnabled: boolean;
  /**
   * Onboarding guide video (Support page player + its VideoObject JSON-LD).
   * '' = this brand has no guide video: the player and the JSON-LD block are
   * both omitted rather than falling back to another brand's asset.
   */
  guideVideoUrl: string;
  /** App store listings. '' = not published yet: the download page shows a
   *  "coming soon" state instead of a dead link. */
  storeLinks: { ios: string; android: string };
  /** Download CDN layout (`/kaitu/` path segment, `Kaitu_*` artifacts). */
  cdn: {
    desktopBases: readonly string[];
    mobileBases: readonly string[];
    artifactPrefix: string;
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
  privacyEmail: 'privacy@kaitu.io',
  legalEmail: 'legal@kaitu.io',
  ogImagePath: '/images/og-default.png',
  taglineZh: '愿上帝为你开路',
  productName: '开途 VPN',
  faviconPrefix: '',
  gaMeasurementId: 'G-EH2PY4S0CX',
  chatwootToken: 'ZfFNvQRuoKzkik6X4KCSgp1h',
  chatEnabled: true,
  guideVideoUrl: 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/kaitu_guide.mp4',
  // Android ships as an APK from the CDN (androidApkGuide), not a store listing.
  storeLinks: { ios: 'https://apps.apple.com/app/id6448744655', android: '' },
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
};

/**
 * The other brand, as the cross-brand admin dashboard (/manager) and the
 * cross-layer contract (tests/cross-layer-contract.test.ts) see it. overleap.io
 * itself is the standalone app in `sites/overleap/` — this site never renders
 * an overleap page, so only identity fields live here.
 */
export const OVERLEAP: BrandIdentity = {
  id: 'overleap',
  displayName: 'Overleap',
  wordmark: 'Overleap',
  baseUrl: 'https://overleap.io',
  contactEmail: 'support@overleap.io',
};

/** Registry lookup for cross-brand surfaces (manager brand filter / badges). */
export function brandById(id: BrandId): BrandIdentity {
  return id === 'overleap' ? OVERLEAP : KAITU;
}

/**
 * The brand this site serves. web/ is kaitu.io only — overleap.io is the
 * standalone app in `sites/overleap/` — so this is a constant; it stays a
 * function so call sites read the brand from one place.
 */
export function siteBrand(): Brand {
  return KAITU;
}
