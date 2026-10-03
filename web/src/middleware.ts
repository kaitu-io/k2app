import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { siteBrand } from './lib/brands';

type Locale = (typeof routing.locales)[number];

const intlMiddleware = createMiddleware(routing);

// Locales this site used to serve when it also built the overleap site (en-* / ja).
// Their URLs are still indexed / bookmarked: 301 them to the same path under
// zh-CN rather than letting next-intl treat `en-US` as a path segment.
const LEGACY_LOCALE_RE = /^\/(en-US|en-GB|en-AU|ja)(\/.*)?$/;

export default function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const brand = siteBrand();

  // ---- API proxy: inject the brand for the Center API. ---------------------
  // Center resolves brand as Host → X-K2-Brand → kaitu (api/brand.go); the
  // proxied request's Host is the backend origin, so the header is what
  // carries the brand end-to-end.
  if (pathname.startsWith('/api/') || pathname.startsWith('/app/')) {
    const requestHeaders = new Headers(request.headers);
    requestHeaders.set('X-K2-Brand', brand.id);
    return NextResponse.next({ request: { headers: requestHeaders } });
  }

  // ---- Browsers request /favicon.ico unconditionally; the root file is the
  // icon. It must return here rather than fall through to intlMiddleware
  // below — next-intl's default localePrefix ('always') redirects any
  // non-prefixed path to `/${defaultLocale}${pathname}`, which for
  // /favicon.ico has no matching route and resolves as a dead page instead of
  // the icon. -----------------------------------------------------------------
  if (pathname === '/favicon.ico') {
    return NextResponse.next();
  }

  // ---- Install scripts (Linux install / k2s / k2r). -------------------------
  if (pathname === '/i/k2' || pathname === '/i/k2s' || pathname === '/i/k2r') {
    if (pathname === '/i/k2s' || pathname === '/i/k2r') {
      const ip = request.headers.get('x-forwarded-for')?.split(',')[0]?.trim()
        || request.headers.get('x-real-ip')
        || 'unknown';
      const ua = request.headers.get('user-agent') || '';
      const endpoint = pathname === '/i/k2s' ? 'k2s-download' : 'k2r-download';
      fetch(`https://k2.52j.me/api/stats/${endpoint}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip_raw: ip, ua }),
      }).catch(() => {
        // Non-blocking, ignore errors
      });
    }
    return NextResponse.next();
  }

  // ---- Admin surfaces (/manager) bypass i18n entirely. ----------------------
  if (pathname.startsWith('/manager')) {
    return NextResponse.next();
  }

  // ---- Legacy en-* / ja URLs → 301 to the same path under the default
  // locale, same host. ---------------------------------------------------------
  const legacyMatch = pathname.match(LEGACY_LOCALE_RE);
  if (legacyMatch) {
    const rest = legacyMatch[2] ?? '';
    const targetUrl = new URL(`/${brand.defaultLocale}${rest}`, request.url);
    targetUrl.search = request.nextUrl.search;
    return NextResponse.redirect(targetUrl, 301);
  }

  // ---- Root path → pick locale within the brand's allowed set. ------------
  if (pathname === '/') {
    const allowedSet = new Set<string>(brand.allowedLocales);
    const preferredLocale = request.cookies.get('preferredLocale')?.value;
    if (preferredLocale && allowedSet.has(preferredLocale)) {
      const response = NextResponse.redirect(new URL(`/${preferredLocale}`, request.url));
      response.headers.set('Cache-Control', 'private, no-store, must-revalidate');
      return response;
    }

    const acceptLanguage = request.headers.get('accept-language');
    const detectedLocale = getBestLocale(acceptLanguage, brand.allowedLocales);

    const response = NextResponse.redirect(new URL(`/${detectedLocale}`, request.url));
    // The redirect target depends on Accept-Language + cookies; must not be
    // publicly cached or CloudFront pins one PoP-wide answer for everybody.
    response.headers.set('Cache-Control', 'private, no-store, must-revalidate');

    if (!request.cookies.get('hasVisited')) {
      response.cookies.set('hasVisited', 'true', {
        httpOnly: false,
        secure: process.env.NODE_ENV === 'production',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24 * 365, // 1 year
      });
      response.cookies.set('suggestedLocale', detectedLocale, {
        httpOnly: false,
        secure: process.env.NODE_ENV === 'production',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24, // 24 hours
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
    pathname.replace(/^\/(zh-CN|zh-TW|zh-HK)(?=\/|$)/, '') || '/';
  if (response && typeof (response as Response).headers?.set === 'function') {
    (response as Response).headers.set('x-middleware-request-x-pathname', strippedPathname);
  }
  return response;
}

// Get the best matching locale based on Accept-Language header, constrained to
// allowedLocales; anything unmatched (en, ja, …) lands on the default zh-CN.
function getBestLocale(
  acceptLanguage: string | null,
  allowedLocales: readonly Locale[]
): Locale {
  const allowedSet = new Set<string>(allowedLocales);
  const fallback = routing.defaultLocale as Locale;

  if (!acceptLanguage) return fallback;

  const languages = acceptLanguage.split(',').map(lang => {
    const [code, q = '1'] = lang.trim().split(';q=');
    return {
      code: code.toLowerCase(),
      quality: parseFloat(q.replace('q=', ''))
    };
  }).sort((a, b) => b.quality - a.quality);

  for (const lang of languages) {
    const exact = routing.locales.find(locale => locale.toLowerCase() === lang.code);
    if (exact && allowedSet.has(exact)) {
      return exact as Locale;
    }

    const langPrefix = lang.code.split('-')[0];
    const langSuffix = lang.code.split('-')[1];

    if (langPrefix === 'zh') {
      const zhPick =
        langSuffix === 'hk' || langSuffix === 'mo' ? 'zh-HK'
          : langSuffix === 'tw' ? 'zh-TW'
            : 'zh-CN';
      if (allowedSet.has(zhPick)) return zhPick as Locale;
    }
  }

  return fallback;
}

export const config = {
  // /api, /app, /manager, /favicon.ico MUST hit the middleware (X-K2-Brand
  // injection, /manager passthrough). The legacy en-* / ja prefixes are listed
  // so their 301 also covers dotted paths the catch-all excludes. Static
  // assets and _next remain excluded via the catch-all.
  matcher: [
    '/',
    '/favicon.ico',
    '/(zh-CN|zh-TW|zh-HK)/:path*',
    '/(en-GB|en-US|en-AU|ja)/:path*',
    '/(api|app)/:path*',
    '/manager/:path*',
    '/manager',
    '/((?!api|app|manager|_next|_vercel|.*\\..*).*)',
  ],
};
