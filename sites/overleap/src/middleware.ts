import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';
import { LOCALES, DEFAULT_LOCALE, SITE, isLocale, type Locale } from './lib/site';

const intlMiddleware = createMiddleware(routing);

const NO_STORE = 'private, no-store, must-revalidate';

// Locale-looking first segments we do not serve (zh-CN, en-IE, nl …) → 301 to
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

// Legacy ISO 639 codes some Android clients still send.
const TAG_ALIASES: Record<string, string> = { in: 'id' };

/**
 * Pick a served locale from Accept-Language. Per tag, in q order: the tag
 * itself, then each parent (`fr-CA` → `fr`), then the first served locale of
 * the same language (`pt-PT` → `pt-BR`; any English variant we do not serve —
 * bare `en`, en-IE, en-NZ … — lands on the en-GB master because it is listed
 * first). Only when no tag matches does the default apply.
 */
export function negotiateLocale(acceptLanguage: string | null): Locale {
  if (!acceptLanguage) return DEFAULT_LOCALE;
  const ranked = acceptLanguage
    .split(',')
    .map((part) => {
      const [tag, q] = part.trim().split(';q=');
      return { tag: tag.trim().toLowerCase(), q: q ? parseFloat(q) : 1 };
    })
    .filter((l) => l.tag && l.tag !== '*' && l.q > 0)
    .sort((a, b) => b.q - a.q);

  const byLower = new Map(LOCALES.map((l) => [l.toLowerCase(), l] as const));
  for (const { tag } of ranked) {
    for (let t = tag; t; t = t.includes('-') ? t.slice(0, t.lastIndexOf('-')) : '') {
      const hit = byLower.get(TAG_ALIASES[t] ?? t);
      if (hit) return hit;
    }
    const primary = tag.split('-')[0];
    const sameLanguage = LOCALES.find((l) => l.toLowerCase().split('-')[0] === primary);
    if (sameLanguage) return sameLanguage;
  }
  return DEFAULT_LOCALE;
}

export const config = {
  matcher: ['/', '/favicon.ico', '/(api|app)/:path*', '/((?!api|app|_next|_vercel|.*\\..*).*)'],
};
