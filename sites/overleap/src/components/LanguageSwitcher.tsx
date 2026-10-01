'use client';

import { useMemo, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Check, Globe, Search } from 'lucide-react';
import { usePathname, useRouter } from '@/i18n/routing';
import { LOCALES, LOCALE_META, filterLocales, type Locale } from '@/lib/site';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';

export default function LanguageSwitcher() {
  const locale = useLocale() as Locale;
  const t = useTranslations('nav');
  const router = useRouter();
  const pathname = usePathname();
  const [query, setQuery] = useState('');

  // Current language first, so it stays visible however long the list grows.
  const ordered = useMemo(() => [locale, ...LOCALES.filter((l) => l !== locale)], [locale]);
  const matches = useMemo(() => filterLocales(query, ordered), [query, ordered]);

  function choose(next: Locale) {
    document.cookie = `preferredLocale=${next}; path=/; max-age=31536000; samesite=lax`;
    router.replace(pathname, { locale: next });
  }

  return (
    <DropdownMenu dir={LOCALE_META[locale].dir} onOpenChange={(open) => !open && setQuery('')}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-1.5 text-muted-foreground" aria-label={t('language')}>
          <Globe className="h-4 w-4" />
          <span className="hidden sm:inline">{LOCALE_META[locale].nativeName}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64 p-0">
        <div className="flex items-center gap-2 border-b px-3">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            // The menu treats keystrokes as type-ahead and would steal focus
            // from the field; only navigation keys may reach it.
            onKeyDown={(e) => {
              if (!['ArrowDown', 'ArrowUp', 'Escape', 'Tab'].includes(e.key)) e.stopPropagation();
            }}
            placeholder={t('searchLanguage')}
            aria-label={t('searchLanguage')}
            className="h-10 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {matches.length === 0 ? (
            <p className="px-2 py-6 text-center text-sm text-muted-foreground">{t('noLanguageFound')}</p>
          ) : (
            matches.map((l) => {
              const { nativeName, englishName, dir } = LOCALE_META[l];
              return (
                <DropdownMenuItem
                  key={l}
                  onClick={() => choose(l)}
                  aria-current={l === locale ? 'true' : undefined}
                  className="justify-between gap-3"
                >
                  <span className="flex flex-col">
                    <span lang={l} dir={dir}>
                      {nativeName}
                    </span>
                    {englishName !== nativeName && (
                      <span className="text-xs text-muted-foreground">{englishName}</span>
                    )}
                  </span>
                  {l === locale && <Check className="h-4 w-4 shrink-0" />}
                </DropdownMenuItem>
              );
            })
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
