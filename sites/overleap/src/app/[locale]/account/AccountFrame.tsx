'use client';

import { useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { CreditCard, Lock, LogOut } from 'lucide-react';
import { Link, usePathname, useRouter } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { profileEmail } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const ITEMS = [
  { href: '/account', labelKey: 'navSubscription', icon: CreditCard },
  { href: '/account/security', labelKey: 'navSecurity', icon: Lock },
] as const;

/** Signed-in guard + account navigation. Signed-out visitors go to /login?next=<here>. */
export default function AccountFrame({ children }: { children: React.ReactNode }) {
  const t = useTranslations('account');
  const router = useRouter();
  const pathname = usePathname();
  const { profile, loading, logout } = useAuth();

  useEffect(() => {
    if (!loading && !profile) {
      const here = pathname + (typeof window !== 'undefined' ? window.location.search : '');
      router.replace(`/login?next=${encodeURIComponent(here)}`);
    }
  }, [loading, profile, pathname, router]);

  if (loading || !profile) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center" role="status" aria-label={t('loading')}>
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl px-4 py-10 sm:px-6">
      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-muted-foreground">{profileEmail(profile)}</p>
      </div>
      <div className="flex flex-col gap-8 md:flex-row">
        <nav aria-label={t('title')} className="flex gap-2 md:w-52 md:shrink-0 md:flex-col">
          {ITEMS.map(({ href, labelKey, icon: Icon }) => {
            const active = pathname === href;
            return (
              <Link
                key={href}
                href={href}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors',
                  active ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                )}
              >
                <Icon className="h-4 w-4" />
                {t(labelKey)}
              </Link>
            );
          })}
          <Button
            variant="ghost"
            className="justify-start gap-2 px-3 text-sm font-normal text-muted-foreground md:mt-4"
            onClick={async () => {
              await logout();
              router.replace('/');
            }}
          >
            <LogOut className="h-4 w-4" />
            {t('signOut')}
          </Button>
        </nav>
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  );
}
