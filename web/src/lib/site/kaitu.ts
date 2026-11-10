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
      { labelKey: 'nav.nav.pricing', href: '/pricing' },
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
        { labelKey: 'nav.nav.pricing', href: '/pricing' },
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
        { labelKey: 'nav.footer.optOut', href: '/api/px/optout', external: true },
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
    '/pricing',
    '/privacy',
    '/purchase',
    '/purchase/router',
    '/releases',
    '/routers',
    '/support',
    '/terms',
  ],
  // /pricing 的 App 版价表快照（美分），pid 对应 Center plans 表；线上价由页面加载后的 /api/plans 覆盖。
  // 2026-09-24 线上值：1y $49（原 $59）· 2y $94.9（原 $128，热门）· 3y $139（原 $177）· 5y $199（原 $295）。
  appPlans: [
    { pid: '1y', months: 12, price: 4900, originPrice: 5900, highlight: false },
    { pid: '2y', months: 24, price: 9490, originPrice: 12800, highlight: true },
    { pid: '3y', months: 36, price: 13900, originPrice: 17700, highlight: false },
    { pid: '5y', months: 60, price: 19900, originPrice: 29500, highlight: false },
  ],
  contentCategories: {
    guides: {
      name: {
        'zh-CN': '使用指南',
        'zh-TW': '使用指南',
        'zh-HK': '使用指南',
        'en-US': 'Guides',
        ko: '이용 가이드',
        ja: 'ご利用ガイド',
        ru: 'Руководства',
      },
      description: {
        'zh-CN': '使用方法、最佳实践和故障排查指南',
        'zh-TW': '使用方法、最佳實踐與故障排查指南',
        'zh-HK': '使用方法、最佳實踐與故障排查指南',
        'en-US': 'How to get online in China: setup, best practices and troubleshooting',
        ko: '중국에서 인터넷을 자유롭게 쓰는 방법: 설정, 활용 팁, 문제 해결',
        ja: '中国でネットを快適に使うために：設定方法・活用のコツ・トラブル対処',
        ru: 'Как пользоваться интернетом в Китае: настройка, советы и решение проблем',
      },
      sections: [
        { key: 'kaitu', name: { 'zh-CN': '{wordmark}入门与使用', 'zh-TW': '{wordmark}入門與使用', 'zh-HK': '{wordmark}入門與使用', 'en-US': 'Getting started with {wordmark}', ko: '{wordmark} 시작하기', ja: '{wordmark} の使い方', ru: 'Как начать работу с {wordmark}' } },
        { key: 'study', name: { 'zh-CN': '国际学校、留学与网课', 'zh-TW': '國際學校、留學與網課', 'zh-HK': '國際學校、留學與網課', 'en-US': 'School, study and online courses', ko: '학교·유학·온라인 강의', ja: '学校・留学・オンライン授業', ru: 'Учёба и онлайн-курсы' } },
        { key: 'apple-id', name: { 'zh-CN': '海外 Apple ID', 'zh-TW': '海外 Apple ID', 'zh-HK': '海外 Apple ID', 'en-US': 'Apple ID', ko: 'Apple ID', ja: 'Apple ID', ru: 'Apple ID' } },
        // Sections for foreign-language guides (posts written for foreigners in China).
        { key: 'living-in-china', name: { 'zh-CN': '在华生活上网', 'zh-TW': '在華生活上網', 'zh-HK': '在華生活上網', 'en-US': 'Living in China', ko: '중국 생활과 인터넷', ja: '中国生活とネット', ru: 'Жизнь в Китае' } },
      ],
    },
  },
  seo: {
    defaultTitle: {
      'zh-CN': '{wordmark} k2cc — 为 26% 丢包设计的隐身隧道',
      'zh-TW': '{wordmark} k2cc — 為 26% 丟包設計的隱身隧道',
      'zh-HK': '{wordmark} k2cc — 為 26% 丟包設計的隱身隧道',
      'en-US': '{wordmark} — VPN that works in China, built for lossy cross-border links',
      ko: '{wordmark} — 중국에서도 잘 되는 VPN, 손실 많은 국경 회선을 위해 설계',
      ja: '{wordmark} — 中国でつながる VPN、パケットロスの多い国際回線のために設計',
      ru: '{wordmark} — VPN, который работает в Китае, для линий с высокими потерями',
    },
    defaultDescription: {
      'zh-CN': 'k2cc 重写拥塞控制规则，为 26% 丢包的跨境线路设计。ECH 加密隐身 + QUIC/TCP-WS 双栈传输，一行命令部署，CT 日志零暴露。',
      'zh-TW': 'k2cc 重寫擁塞控制規則，為 26% 丟包的跨境線路設計。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
      'zh-HK': 'k2cc 重寫擁塞控制規則，為 26% 丟包的跨境線路設計。ECH 加密隱身 + QUIC/TCP-WS 雙棧傳輸，一行命令部署，CT 日誌零暴露。',
      'en-US': 'Reach Google, WhatsApp, YouTube and the rest of the open internet from China. k2cc congestion control is built for lossy cross-border links; ECH stealth with QUIC/TCP-WS dual transport.',
      ko: '중국에서 카카오톡, 네이버, 유튜브, 구글을 그대로 이용하세요. k2cc 혼잡 제어는 손실이 많은 국경 회선을 위해 설계되었으며 ECH 은닉과 QUIC/TCP-WS 이중 전송을 지원합니다.',
      ja: '中国から LINE、Google、YouTube にそのままアクセス。k2cc 輻輳制御はパケットロスの多い国際回線のために設計され、ECH による秘匿と QUIC/TCP-WS の二重トランスポートに対応しています。',
      ru: 'Telegram, WhatsApp, YouTube и Google из Китая без ограничений. Управление перегрузкой k2cc создано для линий с высокими потерями; маскировка ECH и двойной транспорт QUIC/TCP-WS.',
    },
  },
};
