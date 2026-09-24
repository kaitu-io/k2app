import type { SiteConfig } from './types';

/**
 * kaitu 站结构。
 *
 * 顶栏遵循产品站的固定形态（功能 · 定价 · 路由器版 · 帮助 · 开发者 + 登录 + 下载 CTA），
 * 每个一级项都是真实页面或带子项的分组，不再把首页锚点当导航。同一标签在顶栏与
 * 页脚必须指向同一路径（tests/site-config-keys.test.ts 锁住）。
 */
export const KAITU_SITE: SiteConfig = {
  nav: {
    primary: [
      {
        labelKey: 'nav.nav.features',
        href: '/#features',
        children: [
          { labelKey: 'nav.nav.whyBrand', href: '/#features' },
          { labelKey: 'nav.nav.whyTestimonials', href: '/#testimonials' },
          { labelKey: 'nav.footer.product.clientDownload', href: '/install' },
        ],
      },
      { labelKey: 'nav.nav.pricing', href: '/purchase' },
      { labelKey: 'nav.nav.routerEdition', href: '/routers' },
      {
        labelKey: 'nav.nav.help',
        href: '/guides',
        children: [
          { labelKey: 'nav.footer.support.userGuide', href: '/guides' },
          { labelKey: 'nav.nav.introGuide', href: '/support' },
          { labelKey: 'nav.nav.faq', href: '/support#faq' },
          { labelKey: 'nav.footer.developer.changelog', href: '/releases' },
        ],
      },
      {
        labelKey: 'nav.footer.developer.title',
        href: '/k2',
        children: [
          { labelKey: 'nav.nav.k2Protocol', href: '/k2' },
          { labelKey: 'nav.nav.quickstart', href: '/k2/quickstart' },
          { labelKey: 'nav.nav.openSource', href: '/opensource' },
          { labelKey: 'nav.footer.developer.github', href: 'https://github.com/getoverleap' },
        ],
      },
    ],
    cta: { labelKey: 'nav.nav.freeDownload', href: '/install' },
  },
  footer: [
    {
      titleKey: 'nav.footer.product.title',
      items: [
        { labelKey: 'nav.footer.product.clientDownload', href: '/install' },
        { labelKey: 'nav.nav.pricing', href: '/purchase' },
        { labelKey: 'nav.nav.routerEdition', href: '/routers' },
        { labelKey: 'nav.footer.product.retailerProgram', href: '/retailer/rules' },
        { labelKey: 'nav.footer.developer.changelog', href: '/releases' },
      ],
    },
    {
      titleKey: 'nav.footer.developer.title',
      items: [
        { labelKey: 'nav.nav.k2Protocol', href: '/k2' },
        { labelKey: 'nav.nav.quickstart', href: '/k2/quickstart' },
        { labelKey: 'nav.nav.openSource', href: '/opensource' },
        { labelKey: 'nav.footer.developer.github', href: 'https://github.com/getoverleap' },
      ],
    },
    {
      titleKey: 'nav.footer.support.title',
      items: [
        { labelKey: 'nav.footer.support.userGuide', href: '/guides' },
        { labelKey: 'nav.nav.introGuide', href: '/support' },
        { labelKey: 'nav.nav.faq', href: '/support#faq' },
        { labelKey: 'nav.footer.support.contact', href: '/support#contact' },
      ],
    },
    {
      titleKey: 'nav.footer.legal.title',
      items: [
        { labelKey: 'discovery.privacy.title', href: '/privacy' },
        { labelKey: 'discovery.terms.title', href: '/terms' },
        { labelKey: 'discovery.deleteAccount.title', href: '/delete-account' },
      ],
    },
  ],
  staticRoutes: [
    '',
    '/login',
    '/discovery',
    '/install',
    '/opensource',
    '/delete-account',
    '/privacy',
    '/purchase',
    '/purchase/router',
    '/releases',
    '/routers',
    '/support',
    '/terms',
  ],
  contentCategories: {
    guides: {
      name: {
        'zh-CN': '使用指南',
        'zh-TW': '使用指南',
        'zh-HK': '使用指南',
      },
      description: {
        'zh-CN': '使用方法、最佳实践和故障排查指南',
        'zh-TW': '使用方法、最佳實踐與故障排查指南',
        'zh-HK': '使用方法、最佳實踐與故障排查指南',
      },
    },
  },
  seo: {
    defaultTitle: {
      'zh-CN': '{wordmark} k2cc — 30% 丢包照样满速的隐身隧道',
      'zh-TW': '{wordmark} k2cc — 30% 丟包照樣滿速的隱身隧道',
      'zh-HK': '{wordmark} k2cc — 30% 丟包照樣滿速的隱身隧道',
    },
    defaultDescription: {
      'zh-CN': 'k2cc 重写拥塞控制规则，30% 丢包依然满速。ECH 加密隐身 + QUIC/TCP-WS 双栈传输，一行命令部署，CT 日志零暴露。',
      'zh-TW': 'k2cc 重寫擁塞控制規則，30% 丟包依然滿速。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
      'zh-HK': 'k2cc 重寫擁塞控制規則，30% 丟包依然滿速。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
    },
  },
};
