import type { WebappBrandConfig } from '../types';
import { KAITU_THEME } from './theme';

/** 开途 / Kaitu — China-market brand. Values mirror the pre-split product. */
export const KAITU_BRAND: WebappBrandConfig = {
  id: 'kaitu',
  productName: 'Kaitu',
  domainLabel: 'Kaitu.io',
  baseURL: 'https://www.kaitu.io',
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
  iapProductIds: ['io.kaitu.sub.basic.1y'],
  faqExtraKeys: ['allNationConnect', 'chinaAppStore'],
  // jsdelivr 镜像组（原 services/antiblock.ts CDN_SOURCES 原样搬移；
  // gh org 路径是基础设施地址，非用户面品牌词）。
  antiblockCdnSources: [
    'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
    'https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
    'https://testingcf.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
    'https://gcore.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
    'https://cdn.jsdmirror.com/gh/kaitu-io/ui-theme@dist/config.js',
    'https://cdn.jsdmirror.cn/gh/kaitu-io/ui-theme@dist/config.js',
    'https://jsd.onmicrosoft.cn/gh/kaitu-io/ui-theme@dist/config.js',
  ],
  theme: KAITU_THEME,
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
