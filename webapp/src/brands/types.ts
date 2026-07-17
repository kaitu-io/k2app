/**
 * Brand type contracts for the dual-brand split (开途/Kaitu vs Overleap).
 *
 * Brand is baked at BUILD TIME via the Vite define __K2_BRAND__ (env K2_BRAND,
 * default 'kaitu'). There is no runtime brand switching.
 *
 * Spec: docs/superpowers/specs/2026-07-14-brand-split-design.md §4
 * Backend counterpart: api/brand.go (BrandConfig registry). IDs must match.
 */
// import type only — erased at runtime, no import cycle with i18n.
import type { LanguageCode } from '../i18n/i18n';

export type BrandId = 'kaitu' | 'overleap';

/** Feature gates that differ between brands. Platform-static features
 *  (proHistory, feedback, appBypass, ...) stay in config/apps.ts. */
export interface BrandFeatures {
  /** Invite-code program (tab, routes, share links). Kaitu-only. */
  invite: boolean;
  /** Retailer (分销商) UI surfaces. Kaitu-only. */
  retailer: boolean;
  /** Discover tab (embeds the brand website's discovery page). */
  discover: boolean;
  /** Delegate-pay setup. Kaitu-only. */
  delegate: boolean;
  /** WordGate order/pay flow on the Purchase page. Kaitu-only. */
  wordgatePurchase: boolean;
  /** Stripe Checkout entry (Phase 6 wires the actual flow; gate reserved now). */
  stripeCheckout: boolean;
  /** Chatwoot support chat widget. */
  chatwoot: boolean;
  /** Dedicated private-node management page. Kaitu-only for now. */
  privateNode: boolean;
  /** Antiblock relay presentation layer (relay transport itself is engine-side).
   *  Overleap users are not behind the GFW; keep UI noise off. */
  antiblockRelay: boolean;
  /** Self-hosted tunnels page (k2s node deploy + k2v5 URI paste).
   *  Gated because the k2s install script is only hosted on kaitu.io — an
   *  overleap build must not surface a `curl kaitu.io/...` command (brand
   *  leakage). Flip on for overleap once overleap.io mirrors /i/k2s. */
  selfHostedTunnels: boolean;
}

interface PaletteTriple {
  main: string;
  light: string;
  dark: string;
}

export interface BrandThemeTokens {
  light: { primary: PaletteTriple; secondary: PaletteTriple };
  dark: { primary: PaletteTriple; secondary: PaletteTriple };
}

export interface WebappBrandConfig {
  id: BrandId;
  /** Latin product name — alt text, share titles, window title. */
  productName: string;
  /** Display domain label, e.g. shown under the login logo. */
  domainLabel: string;
  /** Website base URL — the ONLY fallback for appLinks/invite/install links.
   *  Replaces every scattered `|| 'https://kaitu.io'`. */
  baseURL: string;
  supportEmail: string;
  /** One-liner that installs a self-hosted k2s node, shown on the Tunnels page.
   *  Empty string = this brand has no k2s install channel (gate
   *  `features.selfHostedTunnels` off too). Keeping the literal HERE rather
   *  than in Tunnels.tsx is what lets the inactive brand's module — and the
   *  cross-brand domain inside it — be tree-shaken out of the bundle. */
  k2sInstallUrl: string;
  /** Locale-aware display names. zhHans covers zh-CN (简体), zhHant covers
   *  zh-TW/zh-HK (繁體); everything else uses `default`.
   *  Rule: 中文语境禁用裸词 Kaitu → kaitu sets zhHans: '开途' / zhHant: '開途'. */
  names: { default: string; zhHans?: string; zhHant?: string };
  /** Marketing slogan per locale; `default` is the fallback. */
  slogans: { default: string } & Partial<Record<LanguageCode, string>>;
  defaultLocale: LanguageCode;
  locales: LanguageCode[];
  /** StoreKit 自动续订商品 id（与该品牌 iOS app 的 ASC 配置一致）。
   *  technical ids，品牌纯净度豁免（对方品牌字面量不出现于对方构建——
   *  resolver 常量折叠保证 tree-shake）。 */
  iapProductIds: readonly string[];
  /** 品牌专属 FAQ 故事 key（追加在通用集之后），locale 文案在
   *  brands/<id>/locales/<lang>/ticket.json overlay。 */
  faqExtraKeys: readonly string[];
  /** antiblock 入口配置 CDN 镜像（启动时 Happy-Eyeballs 竞速）。
   *  空数组 = 跳过 CDN 竞速，resolveEntry 直接回落 DEFAULT_ENTRY。 */
  antiblockCdnSources: readonly string[];
  theme: BrandThemeTokens;
  features: BrandFeatures;
}
