import type { SiteConfig } from './types';

/**
 * overleap 站结构（spec 2026-09-04-overleap-site-decoupling §3.1）。
 * 只列 overleap 构建里真实存在的页面（page.tsx 共用页 + page.overleap.tsx）。
 */
export const OVERLEAP_SITE: SiteConfig = {
  nav: {
    primary: [
      { labelKey: 'nav.nav.whyBrand', href: '/#features' },
      { labelKey: 'nav.nav.pricing', href: '/#pricing' },
      { labelKey: 'nav.nav.help', href: '/support' },
    ],
    cta: { labelKey: 'nav.nav.download', href: '/install' },
  },
  footer: [
    {
      titleKey: 'nav.footer.product.title',
      items: [
        { labelKey: 'nav.nav.download', href: '/install' },
        { labelKey: 'nav.nav.pricing', href: '/purchase' },
        { labelKey: 'nav.nav.help', href: '/support' },
      ],
    },
    {
      titleKey: 'nav.footer.developer.title',
      items: [
        { labelKey: 'nav.footer.developer.k2Docs', href: '/k2' },
        { labelKey: 'nav.footer.developer.selfDeploy', href: '/k2/quickstart' },
        { labelKey: 'nav.footer.developer.github', href: 'https://github.com/getoverleap' },
      ],
    },
    {
      titleKey: 'nav.footer.company.title',
      items: [
        { labelKey: 'discovery.privacy.title', href: '/privacy' },
        { labelKey: 'discovery.terms.title', href: '/terms' },
        { labelKey: 'nav.nav.contactUs', href: 'mailto:{contactEmail}' },
      ],
    },
  ],
  staticRoutes: ['', '/install', '/purchase', '/support', '/privacy', '/terms', '/login'],
  contentCategories: {
    blog: {
      name: {
        'en-GB': 'Blog',
        'en-US': 'Blog',
        'en-AU': 'Blog',
        ja: 'ブログ',
      },
      description: {
        'en-GB': 'Privacy, networking and product notes from the {displayName} team',
        'en-US': 'Privacy, networking and product notes from the {displayName} team',
        'en-AU': 'Privacy, networking and product notes from the {displayName} team',
        ja: '{displayName} チームによるプライバシー・ネットワーク・プロダクトの記録',
      },
    },
  },
  seo: {
    defaultTitle: {
      'en-GB': 'Private VPN with no logs, fast on any network | {displayName}',
      'en-US': 'Private VPN with no logs, fast on any network | {displayName}',
      'en-AU': 'Private VPN with no logs, fast on any network | {displayName}',
      ja: 'ログを残さないプライベート VPN、どんな回線でも速い | {displayName}',
    },
    defaultDescription: {
      'en-GB': '{displayName} is a private VPN that hides where you go, even from your ISP, keeps no logs, and stays fast on the networks that let you down. Windows, macOS, iOS and Android.',
      'en-US': '{displayName} is a private VPN that hides where you go, even from your ISP, keeps no logs, and stays fast on the networks that let you down. Windows, macOS, iOS and Android.',
      'en-AU': '{displayName} is a private VPN that hides where you go, even from your ISP, keeps no logs, and stays fast on the networks that let you down. Windows, macOS, iOS and Android.',
      ja: '{displayName} は、アクセス先を ISP からも隠し、ログを残さず、不安定な回線でも速さを保つプライベート VPN。Windows・macOS・iOS・Android 対応。',
    },
  },
};
