import {defineRouting} from 'next-intl/routing';
import {createNavigation} from 'next-intl/navigation';
 
export const routing = defineRouting({
  // This site (kaitu) serves Chinese only. The en-* / ja locales left with the
  // overleap site (sites/overleap/); src/middleware.ts 301s their old URLs to zh-CN.
  locales: ['zh-CN', 'zh-TW', 'zh-HK'],

  defaultLocale: 'zh-CN',

  // Only define pathnames if you need localized URLs
  // For now, we keep the same URL structure across all locales
  // pathnames: {
  //   // Only add here if you want different URLs per locale
  //   // e.g., '/about': { 'en': '/about', 'zh-CN': '/关于我们' }
  // }
});
 
// Lightweight wrappers around Next.js' navigation APIs
// that will consider the routing configuration
export const {Link, redirect, usePathname, useRouter, getPathname} =
  createNavigation(routing);