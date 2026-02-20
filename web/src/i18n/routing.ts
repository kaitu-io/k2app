import {defineRouting} from 'next-intl/routing';
import {createNavigation} from 'next-intl/navigation';
 
export const routing = defineRouting({
  // A list of all locales that are supported
  locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],

  // Used when no locale matches - default to Chinese
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