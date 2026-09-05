import type { WebappBrandConfig } from '../types';
import { OVERLEAP_THEME } from './theme';

/**
 * Overleap — overseas brand. Fully isolated from the peer brand: never mentions it.
 * NOTE: this module IS bundled into overleap artifacts, so it must not contain
 * the peer brand's name or domain even in comments — the purity guard
 * (scripts/check-brand-purity.sh) greps built output, and relying on the
 * minifier to strip comments would make that contract load-bearing on a
 * build-tool default. Keep cross-brand references out of this file entirely.
 * Payment: Stripe (website, Phase 6) + IAP with new product ids (Phase 5/6);
 * WordGate is locked to kaitu (backend 405001 enforces this too).
 * Theme palette: working values pending final design sign-off (see plan's open
 * questions) — distinct violet/teal family so a mis-branded build is obvious.
 */
export const OVERLEAP_BRAND: WebappBrandConfig = {
  id: 'overleap',
  productName: 'Overleap',
  domainLabel: 'Overleap.io',
  baseURL: 'https://www.overleap.io',
  supportEmail: 'support@overleap.io',
  // No k2s install channel: the script is hosted on the peer brand's domain
  // only, and an overleap build must never surface a cross-brand domain.
  // Empty + the selfHostedTunnels gate off. Fill in once overleap.io
  // mirrors /i/k2s.
  k2sInstallUrl: '',
  names: { default: 'Overleap' }, // no zh variants — brand name is Overleap in every locale
  // 与 overleap.io 首页同一句（隐私优先叙事，spec 2026-09-04-overleap-site-decoupling §3.2）。
  slogans: {
    default: 'Your browsing is your business.',
    'zh-CN': '你看什么，只关你的事。',
    'zh-TW': '你看什麼，只關你的事。',
    'zh-HK': '你看什麼，只關你的事。',
    ja: '何を見るかは、あなたの自由。',
  },
  defaultLocale: 'en-US',
  locales: ['zh-CN', 'en-US', 'ja', 'zh-TW', 'zh-HK', 'en-AU', 'en-GB'],
  iapProductIds: ['io.overleap.sub.basic.1y'],
  faqExtraKeys: [],
  antiblockCdnSources: [], // 非受限网络市场，无需入口伪装竞速
  theme: OVERLEAP_THEME,
  features: {
    invite: false,
    retailer: false,
    discover: false, // overleap.io discovery page does not exist yet (web Phase 2 ships it; flip then)
    delegate: false,
    wordgatePurchase: false,
    stripeCheckout: true, // Purchase page renders StripePurchasePanel (subscribe/manage)
    chatwoot: false,
    privateNode: false,
    antiblockRelay: false,
    selfHostedTunnels: false, // no k2s install channel for this brand (see k2sInstallUrl)
    multiCountryRouting: true, // serves all countries: geo detection + country picker
  },
};
