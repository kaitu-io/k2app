// Velite builds content alongside Next (argv detection keeps Turbopack from starting it twice).
const isDev = process.argv.includes('dev');
const isBuild = process.argv.includes('build');
if (!process.env.VELITE_STARTED && (isDev || isBuild)) {
  process.env.VELITE_STARTED = '1';
  import('velite').then((m) => m.build({ watch: isDev, clean: !isDev }));
}

import createNextIntlPlugin from 'next-intl/plugin';
import { withSentryConfig } from '@sentry/nextjs';
import type { NextConfig } from 'next';

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts');

const nextConfig: NextConfig = {
  output: 'standalone',
  trailingSlash: false,
  // Lower `static {}` blocks for iOS 16.0-16.3 / Safari < 16.4, which cannot parse them.
  transpilePackages: ['intl-messageformat'],

  // Center API proxy. API_PROXY_TARGET points local dev at a local Center
  // (API_PROXY_TARGET=http://127.0.0.1:5899 yarn dev). Only /api/* — the admin
  // /app/* surface is not served by this site (middleware 404s it).
  async rewrites() {
    const apiTarget = process.env.API_PROXY_TARGET || 'https://k2.52j.me';
    return [{ source: '/api/:path*', destination: `${apiTarget}/api/:path*` }];
  },

  async headers() {
    return [
      {
        source: '/(_next/static|images|icons|brand)/(.*)',
        headers: [{ key: 'Cache-Control', value: 'public, max-age=31536000, immutable' }],
      },
      // `.+` not `.*`: `/` is a per-visitor 307 (see middleware) and must never be shared-cached.
      {
        source: '/((?!_next|images|icons|brand|api).+)',
        headers: [{ key: 'Cache-Control', value: 'public, max-age=3600, must-revalidate' }],
      },
      {
        source: '/(api|app)/(.*)',
        headers: [{ key: 'Cache-Control', value: 'no-cache, no-store, must-revalidate' }],
      },
      { source: '/', headers: [{ key: 'Cache-Control', value: 'private, no-store, must-revalidate' }] },
    ];
  },
};

export default withSentryConfig(withNextIntl(nextConfig), {
  org: 'anc-3w',
  project: 'javascript-nextjs',
  silent: !process.env.CI,
  widenClientFileUpload: true,
  webpack: { treeshake: { removeDebugLogging: true } },
});
