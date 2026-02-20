import { redirect } from '@/i18n/routing';
import { routing } from '@/i18n/routing';

// SSR模式下允许动态渲染
export const dynamic = 'force-dynamic';

export default function RootPage() {
  // SSR模式下重定向到默认locale主页
  redirect({
    href: '/',
    locale: routing.defaultLocale
  });
}