// Velite build integration — starts the velite watcher/builder alongside Next.js.
// Uses process.argv detection for Turbopack compatibility (avoids double-start).
const isDev = process.argv.indexOf('dev') !== -1
const isBuild = process.argv.indexOf('build') !== -1
if (!process.env.VELITE_STARTED && (isDev || isBuild)) {
  process.env.VELITE_STARTED = '1'
  import('velite').then(m => m.build({ watch: isDev, clean: !isDev }))
}

import createNextIntlPlugin from 'next-intl/plugin';
import type { NextConfig } from 'next';
import * as fs from 'fs';
import * as path from 'path';

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts');

// Read versions from root package.json (single source of truth)
// releaseVersion = stable, version = beta (may include prerelease suffix)
const getRootPackageJson = () => {
  try {
    const packagePath = path.join(__dirname, '../package.json');
    const packageContent = fs.readFileSync(packagePath, 'utf8');
    return JSON.parse(packageContent);
  } catch {
    console.warn('Could not read root package.json, using fallback versions');
    return { version: '0.0.0', releaseVersion: '0.0.0' };
  }
};

const rootPkg = getRootPackageJson();
const desktopVersion = rootPkg.releaseVersion || rootPkg.version || '0.0.0';
const betaVersion = rootPkg.version || '0.0.0';
console.log(`🚀 Building with stable: ${desktopVersion}, beta: ${betaVersion}`);

const nextConfig: NextConfig = {
  // Inject desktop version as environment variable at build time
  env: {
    NEXT_PUBLIC_DESKTOP_VERSION: desktopVersion,
    NEXT_PUBLIC_BETA_VERSION: betaVersion,
    NEXT_PUBLIC_DOWNLOAD_BASE_URL: 'https://d0.all7.cc/kaitu/desktop',
  },

  // Force SSR mode - explicitly disable static export
  output: 'standalone',
  trailingSlash: false,
  
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
      // 动态页面短期缓存（排除API路由）
      {
        source: '/((?!_next|images|icons|favicon|app-icons|api|app).*)',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=3600, must-revalidate',
          },
        ],
      },
      // API路由不缓存 (/api/ 和 /app/)
      {
        source: '/(api|app)/(.*)',
        headers: [
          {
            key: 'Cache-Control',
            value: 'no-cache, no-store, must-revalidate',
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

export default withNextIntl(nextConfig);
