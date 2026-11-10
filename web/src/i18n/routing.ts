import {defineRouting} from 'next-intl/routing';
import {createNavigation} from 'next-intl/navigation';
 
export const routing = defineRouting({
  // Chinese for the mainland market, plus the languages of the largest foreign
  // communities in China (2026-11-10: kaitu serves foreigners living in China —
  // it is still a China product, not an overseas one). Same set as the kaitu app
  // (webapp/src/brands/kaitu/index.ts). Old en-GB / en-AU URLs 301 to en-US.
  locales: ['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'ko', 'ja', 'ru'],

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