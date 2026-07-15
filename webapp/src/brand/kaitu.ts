import type { WebappBrandConfig } from './types';

/** 开途 / Kaitu — China-market brand. Values mirror the pre-split product. */
export const KAITU_BRAND: WebappBrandConfig = {
  id: 'kaitu',
  productName: 'Kaitu',
  domainLabel: 'Kaitu.io',
  baseURL: 'https://www.kaitu.io',
  websiteOrigins: ['https://www.kaitu.io', 'https://kaitu.io'],
  supportEmail: 'support@kaitu.me',
  // Verbatim pre-split value (was Tunnels.tsx DEPLOY_COMMAND). k2s install
  // script is k2-protocol infra hosted on kaitu.io only.
  k2sInstallUrl: 'curl -fsSL https://kaitu.io/i/k2s | sudo sh',
  names: { default: 'Kaitu', zhHans: '开途', zhHant: '開途' },
  slogans: {
    default: 'Smooth passage, even in congestion',
    'zh-CN': '越拥堵，越从容',
    'zh-TW': '越擁堵，越從容',
    'zh-HK': '越擁堵，越從容',
    ja: '混雑の中でも、余裕を',
  },
  defaultLocale: 'zh-CN',
  locales: ['zh-CN', 'en-US', 'ja', 'zh-TW', 'zh-HK', 'en-AU', 'en-GB'],
  theme: {
    // Exact values lifted from the current webapp/src/theme.ts palettes.
    light: {
      primary: { main: '#1565C0', light: '#42A5F5', dark: '#0D47A1' },
      secondary: { main: '#00838F', light: '#26C6DA', dark: '#006064' },
    },
    dark: {
      primary: { main: '#42A5F5', light: '#90CAF9', dark: '#1976D2' },
      secondary: { main: '#26C6DA', light: '#4DD0E1', dark: '#0097A7' },
    },
  },
  features: {
    invite: true,
    retailer: true,
    discover: true,
    delegate: true,
    wordgatePurchase: true,
    stripeCheckout: false,
    chatwoot: true,
    privateNode: true,
    antiblockRelay: true,
    selfHostedTunnels: true,
  },
};
