// Velite build integration — starts the velite watcher/builder alongside Next.js.
// Uses process.argv detection for Turbopack compatibility (avoids double-start).
const isDev = process.argv.indexOf('dev') !== -1
const isBuild = process.argv.indexOf('build') !== -1
if (!process.env.VELITE_STARTED && (isDev || isBuild)) {
  process.env.VELITE_STARTED = '1'
  import('velite').then(m => m.build({ watch: isDev, clean: !isDev }))
}

import createNextIntlPlugin from 'next-intl/plugin';
import { withPayload } from '@payloadcms/next/withPayload';
import { withSentryConfig } from '@sentry/nextjs';
import type { NextConfig } from 'next';

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts');

const nextConfig: NextConfig = {
  // Force SSR mode - explicitly disable static export
  output: 'standalone',
  trailingSlash: false,

  // Force these deps through SWC so class static initialization blocks
  // (`static { ... }`) get lowered to property assignments. Without this,
  // their published ESM ships to iOS 16.0-16.3 / Safari < 16.4 verbatim and
  // fails parsing with `SyntaxError: Unexpected token '{'`, blanking the
  // page. Next.js does not transpile node_modules by default; browserslist
  // is honored only for code that goes through SWC.
  //   - intl-messageformat: pulled into every [locale]/* page via next-intl
  //   - @xterm/xterm: SSH terminal on /manager/nodes
  transpilePackages: ['intl-messageformat', '@xterm/xterm'],
  
  // Enable SSR-compatible image optimization for Amplify
  images: {
    remotePatterns: [
      {
        protocol: 'https' as const,
        hostname: 'k2.52j.me',
        port: '',
        pathname: '/**',
      },
    ],
    // Amplify supports optimized images in SSR mode
    unoptimized: false,
  },

  // Redirects for short URLs
  async redirects() {
    return [
      {
        source: '/d',
        destination: '/install',
        permanent: false,
      },
    ];
  },

  // SSR-compatible rewrites for API proxying
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: 'https://k2.52j.me/api/:path*',
      },
      {
        source: '/app/:path*',
        destination: 'https://k2.52j.me/app/:path*',
      },
    ];
  },

  // Amplify SSR 优化缓存策略
  async headers() {
    return [
      // 静态资源长期缓存
      {
        source: '/(_next/static|images|icons|favicon|app-icons)/(.*)',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=31536000, immutable',
          },
        ],
      },
      // 动态页面短期缓存（排除 API 与管理后台 —— manager/payload 含已登录态，不能公开缓存）
      // `.+` 而非 `.*`：根路径 `/` 由中间件按 Accept-Language + Cookie 计算 307，
      // 公共缓存会让首位访客的语言污染整个 CloudFront PoP。
      {
        source: '/((?!_next|images|icons|favicon|app-icons|api|app|manager|payload).+)',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=3600, must-revalidate',
          },
        ],
      },
      // API 与管理后台不缓存 (/api/、/app/、/manager/、/payload/)
      {
        source: '/(api|app|manager|payload)/(.*)',
        headers: [
          {
            key: 'Cache-Control',
            value: 'no-cache, no-store, must-revalidate',
          },
        ],
      },
      // 根路径 `/` 是中间件按请求头计算的 307，不能进 CDN 公共缓存
      {
        source: '/',
        headers: [
          {
            key: 'Cache-Control',
            value: 'private, no-store, must-revalidate',
          },
        ],
      },
    ];
  },
  
  // Enable experimental features for better SSR performance on Amplify
  experimental: {
    // Remove serverActions as it's deprecated in Next.js 15+
  },
};

export default withSentryConfig(
  withPayload(withNextIntl(nextConfig), {
    devBundleServerPackages: false,
  }),
  {
    org: 'anc-3w',
    project: 'javascript-nextjs',
    silent: !process.env.CI,
    widenClientFileUpload: true,
    webpack: {
      treeshake: {
        removeDebugLogging: true,
      },
    },
  },
);
