// Application constants and configuration

// Desktop release version from build-time environment variable
// This reads from client/package.json:releaseVersion via next.config.ts
export const DESKTOP_VERSION = process.env.NEXT_PUBLIC_DESKTOP_VERSION || '0.0.0';
const DOWNLOAD_BASE_URL = process.env.NEXT_PUBLIC_DOWNLOAD_BASE_URL || 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';

export const DOWNLOAD_LINKS = {
  // Mobile apps
  ios: 'https://apps.apple.com/app/id6448744655',
  android: 'https://d13jc1jqzlg4yt.cloudfront.net/waymaker/waymaker-3.0.2-release.apk',

  // Desktop apps - dynamically generated from environment variables
  windows: `${DOWNLOAD_BASE_URL}/${DESKTOP_VERSION}/Kaitu_${DESKTOP_VERSION}_x64.exe`,
  macos: `${DOWNLOAD_BASE_URL}/${DESKTOP_VERSION}/Kaitu_${DESKTOP_VERSION}_universal.pkg`,
} as const;

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

export const GITHUB_LINKS = {
  server: 'https://github.com/kaitu-io/kaitu-server', // Placeholder URL
  protocol: 'https://github.com/kaitu-io/kaitu-protocol', // Placeholder URL
  organization: 'https://github.com/kaitu-io', // Placeholder URL
} as const;

export const EXTERNAL_LINKS = {
  documentation: 'https://docs.kaitu.me',
  support: 'https://support.kaitu.me',
  pricing: 'https://kaitu.me/pricing',
  status: 'https://status.kaitu.me',
  blog: 'https://blog.kaitu.me',
  contact: 'mailto:support@kaitu.me',
} as const;

export const COMPANY_INFO = {
  name: 'Kaitu',
  fullName: '开途 Kaitu',
  tagline: '安全便捷的网络代理解决方案',
  email: 'support@kaitu.me',
  year: new Date().getFullYear(),
} as const;