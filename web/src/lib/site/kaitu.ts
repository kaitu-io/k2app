import type { SiteConfig } from './types';

/**
 * kaitu站结构。每个值都等于 2026-09-04 改造前 Header / Footer / sitemap / content-posts /
 * metadata 的渲染结果——这份配置就是kaitu站"零视觉变化"的对照表，改之前先看
 * tests/footer-brand-gates.test.tsx 与 tests/brand-leak-ssr.test.tsx 的kaitu断言。
 */
export const KAITU_SITE: SiteConfig = {
  nav: {
    primary: [
      {
        labelKey: 'nav.nav.whyBrand',
        href: '/#hero',
        children: [
          { labelKey: 'nav.nav.whySpeed', href: '/#hero' },
          { labelKey: 'nav.nav.whyTech', href: '/#features' },
          { labelKey: 'nav.nav.whyTestimonials', href: '/#testimonials' },
          { labelKey: 'nav.nav.faq', href: '/#faq' },
        ],
      },
      { labelKey: 'nav.nav.pricing', href: '/purchase' },
    ],
    cta: { labelKey: 'nav.nav.freeDownload', href: '/install' },
  },
  footer: [
    {
      titleKey: 'nav.footer.product.title',
      items: [
        { labelKey: 'nav.footer.product.clientDownload', href: '/install' },
        { labelKey: 'nav.footer.product.smartRouter', href: '/routers' },
        { labelKey: 'nav.footer.product.retailerProgram', href: '/retailer/rules' },
        { labelKey: 'changelog.title', href: '/changelog' },
      ],
    },
    {
      titleKey: 'nav.footer.developer.title',
      items: [
        { labelKey: 'nav.footer.developer.k2Docs', href: '/k2' },
        { labelKey: 'nav.footer.developer.selfDeploy', href: '/k2/quickstart' },
        { labelKey: 'nav.footer.developer.routerConfig', href: '/routers' },
        { labelKey: 'nav.footer.developer.github', href: 'https://github.com/getoverleap' },
        { labelKey: 'nav.footer.developer.changelog', href: '/releases' },
      ],
    },
    {
      titleKey: 'nav.footer.support.title',
      items: [
        { labelKey: 'nav.footer.support.userGuide', href: '/guides' },
        { labelKey: 'nav.footer.support.faq', href: '/guides' },
        { labelKey: 'nav.footer.support.contact', href: '/guides' },
        { labelKey: 'nav.footer.support.homeschoolGuide', href: '/support' },
      ],
    },
    {
      titleKey: 'nav.footer.legal.title',
      items: [
        { labelKey: 'discovery.privacy.title', href: '/privacy' },
        { labelKey: 'discovery.terms.title', href: '/terms' },
      ],
    },
  ],
  staticRoutes: [
    '',
    '/login',
    '/discovery',
    '/install',
    '/opensource',
    '/privacy',
    '/purchase',
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
