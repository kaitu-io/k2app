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
  // jsdelivr 镜像组（原 services/antiblock.ts CDN_SOURCES 原样搬移，随 main 的
  // fix/disable-antiblock-relay 同步更新：config.js→ui.js 改名、新增 quantil/
  // Bunny CDN/statically.io 镜像、原 zzko.cn（TLS 证书过期）替换为 Bunny。
  // gh org 路径是基础设施地址，非用户面品牌词）。
  antiblockCdnSources: [
    // jsDelivr 官方边缘（cdn.jsdelivr.net 主域对 CN 已失效，但其余边缘域可用性各异）
    'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://testingcf.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://gcore.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
    // 网宿 CDNetworks 官方边缘 — 历史上的 CN 友好入口（2026-07 内容校验通过）
    'https://quantil.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
    // 国内第三方镜像（jsdMirror = 腾讯云 EdgeOne；海外探测不通属预期）
    'https://cdn.jsdmirror.com/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://cdn.jsdmirror.cn/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://jsd.onmicrosoft.cn/gh/kaitu-io/ui-theme@dist/ui.js',
    // 独立于 jsDelivr 基础设施的边缘，故障域隔离（Bunny CDN + statically.io GitHub 代理）
    // 注：原 jsd.cdn.zzko.cn 于 2026-07 TLS 证书过期（硬失败），替换为 Bunny b-cdn.net。
    'https://jsdelivr.b-cdn.net/gh/kaitu-io/ui-theme@dist/ui.js',
    'https://cdn.statically.io/gh/kaitu-io/ui-theme@dist/ui.js',
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
