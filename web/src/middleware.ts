import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing } from './i18n/routing';

const intlMiddleware = createMiddleware(routing);

export default function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname;

  // Skip middleware for admin and manager routes (no locale prefix needed)
  if (pathname.startsWith('/admin') || pathname.startsWith('/manager')) {
    return NextResponse.next();
  }

  // Check if pathname is root
  if (pathname === '/') {
    // Check for stored language preference in cookie
    const preferredLocale = request.cookies.get('preferredLocale')?.value;
    
    if (preferredLocale && routing.locales.includes(preferredLocale as (typeof routing.locales)[number])) {
      // Redirect to preferred locale
      return NextResponse.redirect(new URL(`/${preferredLocale}`, request.url));
    }
    
    // Get locale from Accept-Language header
    const acceptLanguage = request.headers.get('accept-language');
    const detectedLocale = getBestLocale(acceptLanguage);
    
    // If detected locale is different from default, show language switcher banner
    // This will be handled by the root page component
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

// Get the best matching locale based on Accept-Language header
function getBestLocale(acceptLanguage: string | null): string {
  if (!acceptLanguage) return routing.defaultLocale;
  
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
    // Exact match
    if (routing.locales.includes(lang.code as (typeof routing.locales)[number])) {
      return lang.code;
    }
    
    // Language code match (e.g., 'en' matches 'en-US')
    const langPrefix = lang.code.split('-')[0];
    const langSuffix = lang.code.split('-')[1];
    
    // Special handling for Chinese regions
    if (langPrefix === 'zh') {
      if (langSuffix === 'hk' || langSuffix === 'mo') {
        return 'zh-HK'; // Hong Kong and Macau use Hong Kong Traditional Chinese
      } else if (langSuffix === 'tw') {
        return 'zh-TW'; // Taiwan Traditional Chinese
      } else if (langSuffix === 'cn' || langSuffix === 'sg') {
        return 'zh-CN'; // Mainland China and Singapore use Simplified Chinese
      }
    }
    
    // Special handling for English regions
    if (langPrefix === 'en') {
      if (langSuffix === 'au') {
        return 'en-AU'; // Australia
      } else if (langSuffix === 'gb' || langSuffix === 'uk') {
        return 'en-GB'; // United Kingdom
      } else if (langSuffix === 'us') {
        return 'en-US'; // United States
      }
    }
    
    const matchedLocale = routing.locales.find(locale => 
      locale.toLowerCase().startsWith(langPrefix)
    );
    if (matchedLocale) {
      return matchedLocale;
    }
  }
  
  return routing.defaultLocale;
}

export const config = {
  // Match only internationalized pathnames - exclude API routes (/api/ and /app/)
  // Payload CMS at /manager/cms is handled by the middleware function check above
  matcher: ['/', '/(zh-CN|zh-TW|zh-HK|en-GB|en-US|en-AU|ja)/:path*', '/((?!api|app|_next|_vercel|.*\\..*).*)']
};