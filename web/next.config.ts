import createNextIntlPlugin from 'next-intl/plugin';
import { withPayload } from '@payloadcms/next/withPayload';
import type { NextConfig } from 'next';
import * as fs from 'fs';
import * as path from 'path';

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts');

// Read releaseVersion from client/package.json (monorepo root - single source of truth)
// releaseVersion is the stable published version, separate from development version
const getDesktopVersion = () => {
  try {
    const packagePath = path.join(__dirname, '../client/package.json');
    const packageContent = fs.readFileSync(packagePath, 'utf8');
    const packageJson = JSON.parse(packageContent);
    // Use releaseVersion for stable releases, fallback to version if not set
    return packageJson.releaseVersion || packageJson.version || '0.0.0';
  } catch {
    console.warn('Could not read desktop version, using fallback: 0.0.0');
    return '0.0.0';
  }
};

const desktopVersion = getDesktopVersion();
console.log(`🚀 Building with desktop version: ${desktopVersion}`);

const nextConfig: NextConfig = {
  // Suppress webpack warnings from payload's dynamic requires
  webpack: (config, { isServer }) => {
    // Handle payload's critical dependency warnings (dynamic require expressions)
    if (isServer) {
      config.ignoreWarnings = [
        ...(config.ignoreWarnings || []),
        {
          module: /node_modules\/@payloadcms\/richtext-lexical/,
          message: /Critical dependency/,
        },
        {
          module: /node_modules\/payload\/dist/,
          message: /Critical dependency/,
        },
      ];
    }
    return config;
  },

  // Inject desktop version as environment variable at build time
  env: {
    NEXT_PUBLIC_DESKTOP_VERSION: desktopVersion,
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

// Apply both withPayload and withNextIntl plugins
export default withPayload(withNextIntl(nextConfig));
