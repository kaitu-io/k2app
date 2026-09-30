'use client';

import { useLocale, useTranslations } from 'next-intl';
import { Globe } from 'lucide-react';
import { usePathname, useRouter } from '@/i18n/routing';
import { LOCALES, type Locale } from '@/lib/site';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';

const NAMES: Record<Locale, string> = {
  'en-GB': 'English (UK)',
  'en-US': 'English (US)',
  'en-AU': 'English (Australia)',
  ja: '日本語',
};

export default function LanguageSwitcher() {
  const locale = useLocale() as Locale;
  const t = useTranslations('nav');
  const router = useRouter();
  const pathname = usePathname();

  function choose(next: Locale) {
    document.cookie = `preferredLocale=${next}; path=/; max-age=31536000; samesite=lax`;
    router.replace(pathname, { locale: next });
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-1.5 text-muted-foreground" aria-label={t('language')}>
          <Globe className="h-4 w-4" />
          <span className="hidden sm:inline">{NAMES[locale]}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {LOCALES.map((l) => (
          <DropdownMenuItem key={l} onClick={() => choose(l)} aria-current={l === locale ? 'true' : undefined}>
            {NAMES[l]}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
