import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { KAITU, OVERLEAP, brandFromHost, ownerBrand } from './lib/brands';
import { isProductionHost } from './lib/host-utils';

type Locale = (typeof routing.locales)[number];

const intlMiddleware = createMiddleware(routing);

// Any locale-prefixed request path. Used to decide whether to cross-domain 301.
const LOCALE_PREFIX_RE = /^\/(zh-CN|zh-TW|zh-HK|en-US|en-GB|en-AU|ja)(\/.*)?$/;

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

  // Bidirectional 301 cross-domain redirect.
  // If the URL's locale prefix is "owned" by the other brand (per ownerBrand())
  // AND the current host is a production host, 301 to the owning brand's baseUrl.
  // Dev hosts (localhost, amplify previews) pass through so each environment can
  // exercise any locale without bouncing off-domain.
  const localeMatch = pathname.match(LOCALE_PREFIX_RE);
  if (localeMatch && isProductionHost(host)) {
    const pathLocale = localeMatch[1];
    const rest = localeMatch[2] ?? '';
    const targetBrandId = ownerBrand(pathLocale);
    if (targetBrandId !== brand.id) {
      const targetBrand = targetBrandId === 'kaitu' ? KAITU : OVERLEAP;
      const targetUrl = new URL(`/${pathLocale}${rest}`, targetBrand.baseUrl);
      targetUrl.search = request.nextUrl.search;
      targetUrl.hash = request.nextUrl.hash;
      return NextResponse.redirect(targetUrl, 301);
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

  // For all other routes, use the default next-intl middleware.
  // Inject x-pathname (stripped of locale prefix) into the downstream RSC
  // request so generateMetadata in [locale]/layout.tsx can build correct
  // hreflang alternates + canonical for the actual page path.
  //
  // We use the `x-middleware-request-*` response-header convention:
  // Next.js converts response headers named `x-middleware-request-{X}` into
  // request header `{X}` on the downstream request, surviving through
  // next-intl's internal rewrite/next() response.
  const response = intlMiddleware(request);
  const strippedPathname =
    pathname.replace(/^\/(zh-CN|zh-TW|zh-HK|en-US|en-GB|en-AU|ja)(?=\/|$)/, '') || '/';
  if (response && typeof (response as Response).headers?.set === 'function') {
    (response as Response).headers.set('x-middleware-request-x-pathname', strippedPathname);
  }
  return response;
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
