import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { LOCALES, DEFAULT_LOCALE, SITE, isLocale, type Locale } from './lib/site';

const intlMiddleware = createMiddleware(routing);

const NO_STORE = 'private, no-store, must-revalidate';

// Locale-looking first segments we do not serve (zh-CN, en-IE, fr …) → 301 to
// the same path under the default locale. Real routes never start with xx or xx-XX.
const FOREIGN_LOCALE_RE = /^\/([a-z]{2}(?:-[A-Za-z]{2})?)(\/.*)?$/;

export default function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // Center API proxy (rewritten in next.config.ts). The proxied request's Host
  // is the backend origin, so the header is what carries the brand end-to-end.
  if (pathname.startsWith('/api/')) {
    const headers = new Headers(request.headers);
    headers.set('X-K2-Brand', SITE.brandId);
    return NextResponse.next({ request: { headers } });
  }
  // /app/* is the admin API. It belongs to the other site and is never proxied here.
  if (pathname.startsWith('/app/')) {
    return new NextResponse(null, { status: 404 });
  }

  if (pathname === '/favicon.ico') {
    return NextResponse.rewrite(new URL(`${SITE.faviconPrefix}/favicon-32x32.png`, request.url));
  }

  if (pathname === '/') {
    const cookieLocale = request.cookies.get('preferredLocale')?.value;
    const locale = isLocale(cookieLocale)
      ? cookieLocale
      : negotiateLocale(request.headers.get('accept-language'));
    const response = NextResponse.redirect(new URL(`/${locale}`, request.url));
    // Depends on Accept-Language + cookie: a shared cache would pin one visitor's
    // language for a whole CDN PoP.
    response.headers.set('Cache-Control', NO_STORE);
    return response;
  }

  const match = pathname.match(FOREIGN_LOCALE_RE);
  if (match && !isLocale(match[1])) {
    const target = new URL(`/${DEFAULT_LOCALE}${match[2] ?? ''}`, request.url);
    target.search = request.nextUrl.search;
    return NextResponse.redirect(target, 301);
  }

  return intlMiddleware(request);
}

/** Pick a served locale from Accept-Language. Any English variant we do not
 *  serve (bare `en`, en-IE, en-NZ …) lands on the en-GB master. */
export function negotiateLocale(acceptLanguage: string | null): Locale {
  if (!acceptLanguage) return DEFAULT_LOCALE;
  const ranked = acceptLanguage
    .split(',')
    .map((part) => {
      const [tag, q] = part.trim().split(';q=');
      return { tag: tag.toLowerCase(), q: q ? parseFloat(q) : 1 };
    })
    .filter((l) => l.tag && !Number.isNaN(l.q))
    .sort((a, b) => b.q - a.q);

  for (const { tag } of ranked) {
    const exact = LOCALES.find((l) => l.toLowerCase() === tag);
    if (exact) return exact;
    const [lang, region] = tag.split('-');
    if (lang === 'en') {
      if (region === 'us') return 'en-US';
      if (region === 'au') return 'en-AU';
      return 'en-GB';
    }
    if (lang === 'ja') return 'ja';
  }
  return DEFAULT_LOCALE;
}

export const config = {
  matcher: ['/', '/favicon.ico', '/(api|app)/:path*', '/((?!api|app|_next|_vercel|.*\\..*).*)'],
};
