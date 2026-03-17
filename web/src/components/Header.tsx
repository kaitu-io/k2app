"use client";

import { useAuth } from '@/contexts/AuthContext';
import { Button } from '@/components/ui/button';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import LanguageSwitcher from '@/components/LanguageSwitcher';
import { Github, Download } from 'lucide-react';
import Image from 'next/image';

export default function Header() {
  const { isAuthenticated, user } = useAuth();
  const t = useTranslations();

  return (
    <nav className="border-b bg-background/95 backdrop-blur-sm sticky top-0 z-50">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between items-center h-16">
          <Link href="/" className="flex items-center space-x-2">
            <Image 
              src="/kaitu-icon.png" 
              alt="Kaitu Logo" 
              width={32}
              height={32}
              className="rounded-md"
            />
            <span className="text-xl font-bold text-foreground">{"Kaitu.io"}</span>
          </Link>
          <div className="flex items-center space-x-4">
            {/* Language Switcher */}
            <LanguageSwitcher />

            {/* Intro Guide & Open Source Links */}
            <div className="hidden sm:flex items-center space-x-3">
              <Link
                href="/k2"
                className="text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                {t('nav.nav.k2Protocol')}
              </Link>
              <div className="w-px h-4 bg-border" />
              <Link
                href="/k2/quickstart"
                className="text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                {t('nav.nav.quickstart')}
              </Link>
              <div className="w-px h-4 bg-border" />
              <Link
                href="/routers"
                className="text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                {t('nav.nav.routers')}
              </Link>
              <div className="w-px h-4 bg-border" />
              <Link
                href="/opensource"
                className="text-muted-foreground hover:text-foreground transition-colors"
                title={t('nav.nav.openSource')}
              >
                <Github className="w-5 h-5" />
              </Link>
              <div className="w-px h-4 bg-border" />
              <Link href="/install">
                <Button variant="outline" size="sm" className="border-primary text-primary hover:bg-primary/10 hover:text-primary font-mono text-xs">
                  <Download className="w-3.5 h-3.5 mr-1" />
                  {t('nav.nav.download')}
                </Button>
              </Link>
            </div>
            
{isAuthenticated ? (
              <div className="flex items-center space-x-4">
                <span className="hidden sm:inline text-muted-foreground">{t('nav.nav.welcome')}{", "}{user?.email}</span>
                <Link href="/account">
                  <Button variant="outline" size="sm">{t('admin.account.title')}</Button>
                </Link>
                {user?.isAdmin && (
                  <Link href="/admin">
                    <Button variant="outline" size="sm">{t('nav.nav.adminPanel')}</Button>
                  </Link>
                )}
              </div>
            ) : (
              <Link href="/login">
                <Button>{t('nav.nav.login')}</Button>
              </Link>
            )}
          </div>
        </div>
      </div>
    </nav>
  );
}