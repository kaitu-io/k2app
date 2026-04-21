import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { brandFromHost } from './lib/brands';

type Locale = (typeof routing.locales)[number];

const intlMiddleware = createMiddleware(routing);

const NON_OVERLEAP_LOCALE_RE = /^\/(zh-CN|zh-TW|zh-HK|ja)(\/.*)?$/;

export default function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const host = request.headers.get('host');
  const brand = brandFromHost(host);

  // Serve install scripts as static files (bypass i18n locale redirect)
  if (pathname === '/i/k2') {
    return NextResponse.next();
  }

  // Track k2s download
  if (pathname === '/i/k2s') {
    // Fire-and-forget: record download to Center API
    const ip = request.headers.get('x-forwarded-for')?.split(',')[0]?.trim()
      || request.headers.get('x-real-ip')
      || 'unknown';
    const ua = request.headers.get('user-agent') || '';

    fetch('https://k2.52j.me/api/stats/k2s-download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip_raw: ip, ua }),
    }).catch(() => {
      // Non-blocking, ignore errors
    });

    return NextResponse.next();
  }

  // Skip middleware for admin, manager, and payload routes (no locale prefix needed).
  // `/manager/cms` is covered by the /manager prefix; `/payload/api` is Payload's REST.
  if (
    pathname.startsWith('/admin') ||
    pathname.startsWith('/manager') ||
    pathname.startsWith('/payload')
  ) {
    return NextResponse.next();
  }

  // Overleap.io only serves English locales. Rewrite zh-*/ja URLs to en-US.
  if (brand.id === 'overleap') {
    const match = pathname.match(NON_OVERLEAP_LOCALE_RE);
    if (match) {
      const rest = match[2] ?? '';
      return NextResponse.redirect(new URL(`/en-US${rest}`, request.url), 307);
    }
  }

  // Root path → pick locale
  if (pathname === '/') {
    if (brand.id === 'overleap') {
      // Overleap is English-only. Always route to en-US, no language suggestion cookie.
      return NextResponse.redirect(new URL('/en-US', request.url), 307);
    }

    // Kaitu.io retains Accept-Language + cookie-based detection.
    const preferredLocale = request.cookies.get('preferredLocale')?.value;
    if (preferredLocale && routing.locales.includes(preferredLocale as Locale)) {
      return NextResponse.redirect(new URL(`/${preferredLocale}`, request.url));
    }

    const acceptLanguage = request.headers.get('accept-language');
    const detectedLocale = getBestLocale(acceptLanguage, brand.allowedLocales);

    const response = NextResponse.redirect(new URL(`/${detectedLocale}`, request.url));

    // Set a cookie to indicate first visit for language detection
    if (!request.cookies.get('hasVisited')) {
      response.cookies.set('hasVisited', 'true', {
        httpOnly: false,
        secure: process.env.NODE_ENV === 'production',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24 * 365 // 1 year
      });
      response.cookies.set('suggestedLocale', detectedLocale, {
        httpOnly: false,
        secure: process.env.NODE_ENV === 'production',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24 // 24 hours
      });
    }

    return response;
  }

  // For all other routes, use the default next-intl middleware
  return intlMiddleware(request);
}

// Get the best matching locale based on Accept-Language header, constrained to allowedLocales.
function getBestLocale(
  acceptLanguage: string | null,
  allowedLocales: readonly Locale[]
): Locale {
  const allowedSet = new Set<string>(allowedLocales);
  const fallback: Locale = allowedLocales.includes(routing.defaultLocale as Locale)
    ? (routing.defaultLocale as Locale)
    : allowedLocales[0];

  if (!acceptLanguage) return fallback;

  // Parse Accept-Language header
  const languages = acceptLanguage.split(',').map(lang => {
    const [code, q = '1'] = lang.trim().split(';q=');
    return {
      code: code.toLowerCase(),
      quality: parseFloat(q.replace('q=', ''))
    };
  }).sort((a, b) => b.quality - a.quality);

  // Find best matching locale
  for (const lang of languages) {
    // Exact match (respecting allowedLocales)
    const exact = routing.locales.find(locale => locale.toLowerCase() === lang.code);
    if (exact && allowedSet.has(exact)) {
      return exact as Locale;
    }

    // Language code match (e.g., 'en' matches 'en-US')
    const langPrefix = lang.code.split('-')[0];
    const langSuffix = lang.code.split('-')[1];

    // Special handling for Chinese regions
    if (langPrefix === 'zh') {
      const zhPick =
        langSuffix === 'hk' || langSuffix === 'mo' ? 'zh-HK'
          : langSuffix === 'tw' ? 'zh-TW'
            : langSuffix === 'cn' || langSuffix === 'sg' ? 'zh-CN'
              : 'zh-CN';
      if (allowedSet.has(zhPick)) return zhPick as Locale;
    }

    // Special handling for English regions
    if (langPrefix === 'en') {
      const enPick =
        langSuffix === 'au' ? 'en-AU'
          : langSuffix === 'gb' || langSuffix === 'uk' ? 'en-GB'
            : 'en-US';
      if (allowedSet.has(enPick)) return enPick as Locale;
    }

    const matched = routing.locales.find(locale =>
      locale.toLowerCase().startsWith(langPrefix) && allowedSet.has(locale)
    );
    if (matched) {
      return matched as Locale;
    }
  }

  return fallback;
}

export const config = {
  // Match only internationalized pathnames - exclude API routes (/api/ and /app/)
  matcher: ['/', '/(zh-CN|zh-TW|zh-HK|en-GB|en-US|en-AU|ja)/:path*', '/((?!api|app|manager|payload|_next|_vercel|.*\\..*).*)']
};
