// Kaitu-only surface: the /routers presale page is feature-gated off the
// overleap deployment (Brand.features.routers). Product names are 开途-branded
// by definition — this file is allow-listed in tests/brand-guard.test.ts.

export const ROUTER_PRODUCTS = {
  k2Mini: {
    name: '开途 Mini 智能路由器',
    englishName: 'Kaitu K2 Mini Router',
    slug: 'k2-mini',
    url: 'https://kaitu.io/products/k2-mini/',
    tagline: '轻巧便携，随时随地畅享高速网络',
    description: '超紧凑设计，即插即用，配置简单，适合旅行、桌面和小户型使用',
    features: [
      '超紧凑设计',
      '即插即用',
      '低功耗静音',
      '全球电压支持',
      '便携收纳包'
    ],
    status: 'presale'
  },
  k2001: {
    name: '开途 001 路由器',
    englishName: 'Kaitu K2-001 Router',
    slug: 'k2-001',
    url: 'https://kaitu.io/products/k2-001/',
    tagline: '多应用智能路由器，轻松畅享自由网络',
    description: '支持多应用安装，灵活扩展性，包含开路者服务，简单操作',
    features: [
      '多应用安装支持',
      '灵活扩展性',
      '内置开路者服务',
      '远程管理支持',
      '用户友好设计'
    ],
    status: 'presale'
  }
} as const;
