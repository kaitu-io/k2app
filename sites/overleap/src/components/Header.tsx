'use client';

import { useState } from 'react';
import Image from 'next/image';
import { useTranslations } from 'next-intl';
import { Download, Menu, X } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { Button } from '@/components/ui/button';
import LanguageSwitcher from '@/components/LanguageSwitcher';
import { NAV, SITE } from '@/lib/site';

export default function Header() {
  const t = useTranslations('nav');
  const { profile, loading } = useAuth();
  const [menuOpen, setMenuOpen] = useState(false);
  const close = () => setMenuOpen(false);

  return (
    <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
        <Link href="/" className="flex items-center gap-2 font-semibold">
          <Image src={SITE.logoPath} alt="" width={28} height={28} className="rounded-md" priority />
          <span className="text-lg">{SITE.name}</span>
        </Link>
        <nav aria-label={t('primary')} className="flex items-center gap-1 sm:gap-2">
          <div className="hidden items-center gap-1 md:flex">
            {NAV.primary.map((item) => (
              <Link key={item.href} href={item.href} className="rounded-md px-3 py-2 text-sm text-muted-foreground hover:text-foreground">
                {t(item.labelKey)}
              </Link>
            ))}
          </div>
          <LanguageSwitcher />
          <Button asChild size="sm" className="hidden md:inline-flex">
            <Link href={NAV.cta.href}>
              <Download />
              {t(NAV.cta.labelKey)}
            </Link>
          </Button>
          {!loading && (
            <Button asChild size="sm" variant="outline">
              <Link href={profile ? '/account' : '/login'}>{profile ? t('account') : t('login')}</Link>
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label={t('menu')}
            aria-expanded={menuOpen}
            aria-controls="site-mobile-menu"
            onClick={() => setMenuOpen((o) => !o)}
          >
            {menuOpen ? <X /> : <Menu />}
          </Button>
        </nav>
      </div>
      {menuOpen && (
        <div id="site-mobile-menu" className="border-t px-4 pb-4 pt-2 md:hidden">
          <Link
            href={NAV.cta.href}
            onClick={close}
            className="mb-2 flex items-center justify-center gap-2 rounded-md bg-primary px-3 py-2.5 text-sm font-semibold text-primary-foreground"
          >
            <Download className="h-4 w-4" />
            {t(NAV.cta.labelKey)}
          </Link>
          {NAV.primary.map((item) => (
            <Link key={item.href} href={item.href} onClick={close} className="block rounded-md px-3 py-2.5 text-sm text-muted-foreground hover:text-foreground">
              {t(item.labelKey)}
            </Link>
          ))}
        </div>
      )}
    </header>
  );
}
