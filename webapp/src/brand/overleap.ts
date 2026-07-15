import type { WebappBrandConfig } from './types';

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
  slogans: {
    default: 'Thrives where networks struggle',
    'zh-CN': '网络越难，越显从容',
    'zh-TW': '網路越難，越顯從容',
    'zh-HK': '網絡越難，越顯從容',
    ja: 'ネットワークが苦しむ場所でこそ強い',
  },
  defaultLocale: 'en-US',
  locales: ['zh-CN', 'en-US', 'ja', 'zh-TW', 'zh-HK', 'en-AU', 'en-GB'],
  theme: {
    light: {
      primary: { main: '#5E35B1', light: '#7E57C2', dark: '#4527A0' },
      secondary: { main: '#00897B', light: '#26A69A', dark: '#00695C' },
    },
    dark: {
      primary: { main: '#9575CD', light: '#B39DDB', dark: '#673AB7' },
      secondary: { main: '#4DB6AC', light: '#80CBC4', dark: '#26A69A' },
    },
  },
  features: {
    invite: false,
    retailer: false,
    discover: false, // overleap.io discovery page does not exist yet (web Phase 2 ships it; flip then)
    delegate: false,
    wordgatePurchase: false,
    stripeCheckout: true, // gate reserved; Purchase flow lands in Phase 6
    chatwoot: false,
    privateNode: false,
    antiblockRelay: false,
    selfHostedTunnels: false, // no k2s install channel for this brand (see k2sInstallUrl)
  },
};
