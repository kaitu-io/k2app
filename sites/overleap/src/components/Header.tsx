'use client';

import Image from 'next/image';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { Button } from '@/components/ui/button';
import LanguageSwitcher from '@/components/LanguageSwitcher';
import { NAV, SITE } from '@/lib/site';

export default function Header() {
  const t = useTranslations('nav');
  const { profile, loading } = useAuth();

  return (
    <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
        <Link href="/" className="flex items-center gap-2 font-semibold">
          <Image src={SITE.logoPath} alt="" width={28} height={28} className="rounded-md" priority />
          <span className="text-lg">{SITE.name}</span>
        </Link>
        <nav aria-label={t('primary')} className="flex items-center gap-1 sm:gap-2">
          {NAV.primary.map((item) => (
            <Link key={item.href} href={item.href} className="rounded-md px-3 py-2 text-sm text-muted-foreground hover:text-foreground">
              {t(item.labelKey)}
            </Link>
          ))}
          <LanguageSwitcher />
          {!loading && (
            <Button asChild size="sm" variant={profile ? 'outline' : 'default'}>
              <Link href={profile ? '/account' : '/login'}>{profile ? t('account') : t('login')}</Link>
            </Button>
          )}
        </nav>
      </div>
    </header>
  );
}
