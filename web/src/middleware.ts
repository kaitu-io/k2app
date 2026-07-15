import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { siteBrand } from './lib/brands';

type Locale = (typeof routing.locales)[number];

const intlMiddleware = createMiddleware(routing);

// Any locale-prefixed request path (all 7 codebase locales — the brand gate
// below decides which of them this deployment actually serves).
const LOCALE_PREFIX_RE = /^\/(zh-CN|zh-TW|zh-HK|en-US|en-GB|en-AU|ja)(\/.*)?$/;

export default function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const brand = siteBrand();
  const isKaitu = brand.id === 'kaitu';

  // ---- API proxy: inject the baked brand for the Center API. --------------
  // Center resolves brand as Host → X-K2-Brand → kaitu (api/brand.go); the
  // proxied request's Host is the backend origin, so the header is what
  // carries the brand end-to-end. /app/* is the admin API — kaitu-only.
  if (pathname.startsWith('/api/') || pathname.startsWith('/app/')) {
    if (pathname.startsWith('/app/') && !isKaitu) {
      return new NextResponse(null, { status: 404 });
    }
    const requestHeaders = new Headers(request.headers);
    requestHeaders.set('X-K2-Brand', brand.id);
    return NextResponse.next({ request: { headers: requestHeaders } });
  }

  // ---- Browsers request /favicon.ico unconditionally; the root file is the
  // kaitu icon. Brands with a namespaced favicon set get a rewrite. -------
  if (pathname === '/favicon.ico' && brand.faviconPrefix) {
    return NextResponse.rewrite(
      new URL(`${brand.faviconPrefix}/favicon-32x32.png`, request.url),
    );
  }

  // ---- Install scripts: kaitu-only surface (Linux install / k2s / k2r). ---
  if (pathname === '/i/k2' || pathname === '/i/k2s' || pathname === '/i/k2r') {
    if (!isKaitu) {
      return new NextResponse(null, { status: 404 });
    }
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

  // ---- Admin surfaces live only in the kaitu deployment (internal tools). -
  if (
    pathname.startsWith('/admin') ||
    pathname.startsWith('/manager') ||
    pathname.startsWith('/payload')
  ) {
    if (!isKaitu) {
      return new NextResponse(null, { status: 404 });
    }
    return NextResponse.next();
  }

  // ---- Off-brand locale → 301 to the same path under the brand's default
  // locale, SAME HOST. The old cross-domain 301 is gone: the two brands do
  // not know about each other (spec: 两站互不感知). ------------------------
  const localeMatch = pathname.match(LOCALE_PREFIX_RE);
  if (localeMatch) {
    const pathLocale = localeMatch[1] as Locale;
    if (!(brand.allowedLocales as readonly string[]).includes(pathLocale)) {
      const rest = localeMatch[2] ?? '';
      const targetUrl = new URL(`/${brand.defaultLocale}${rest}`, request.url);
      targetUrl.search = request.nextUrl.search;
      return NextResponse.redirect(targetUrl, 301);
    }
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
    pathname.replace(/^\/(zh-CN|zh-TW|zh-HK|en-US|en-GB|en-AU|ja)(?=\/|$)/, '') || '/';
  if (response && typeof (response as Response).headers?.set === 'function') {
    (response as Response).headers.set('x-middleware-request-x-pathname', strippedPathname);
  }
  return response;
}

// Get the best matching locale based on Accept-Language header, constrained to
// allowedLocales. (Unchanged from the pre-split implementation — it was already
// allowedLocales-constrained; the overleap fallback lands on en-US because
// zh-CN is not in its allowed set, so allowedLocales[0] wins.)
function getBestLocale(
  acceptLanguage: string | null,
  allowedLocales: readonly Locale[]
): Locale {
  const allowedSet = new Set<string>(allowedLocales);
  const fallback: Locale = allowedLocales.includes(routing.defaultLocale as Locale)
    ? (routing.defaultLocale as Locale)
    : allowedLocales[0];

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
            : langSuffix === 'cn' || langSuffix === 'sg' ? 'zh-CN'
              : 'zh-CN';
      if (allowedSet.has(zhPick)) return zhPick as Locale;
    }

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
  // /api, /app, /admin, /manager, /payload, /favicon.ico now MUST hit the
  // middleware (brand gating + X-K2-Brand injection) — the old matcher
  // excluded them. Static assets and _next remain excluded via the catch-all.
  matcher: [
    '/',
    '/favicon.ico',
    '/(zh-CN|zh-TW|zh-HK|en-GB|en-US|en-AU|ja)/:path*',
    '/(api|app)/:path*',
    '/(admin|manager|payload)/:path*',
    '/admin',
    '/manager',
    '/payload',
    '/((?!api|app|admin|manager|payload|_next|_vercel|.*\\..*).*)',
  ],
};
