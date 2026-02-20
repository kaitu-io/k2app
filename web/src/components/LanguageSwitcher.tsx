"use client";

import { useLocale } from 'next-intl';
import { useRouter, usePathname } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { ChevronDown } from 'lucide-react';
import { routing } from '@/i18n/routing';
import Cookies from 'js-cookie';
import FlagIcon from './FlagIcon';
import { useAuth } from '@/contexts/AuthContext';
import { api } from '@/lib/api';

const localeNames = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文 (台灣)',
  'zh-HK': '繁體中文 (香港)',
  'en-US': 'English (US)',
  'en-GB': 'English (UK)',
  'en-AU': 'English (AU)', 
  'ja': '日本語'
} as const;

const localeShortNames = {
  'zh-CN': '简',
  'zh-TW': '繁TW',
  'zh-HK': '繁HK',
  'en-US': 'US',
  'en-GB': 'GB',
  'en-AU': 'AU',
  'ja': '日'
} as const;

export default function LanguageSwitcher() {
  const locale = useLocale();
  const router = useRouter();
  const pathname = usePathname();
  const { isAuthenticated } = useAuth();

  const switchLocale = async (newLocale: string) => {
    // Save the user's language preference
    Cookies.set('preferredLocale', newLocale, { expires: 365 });

    // If user is logged in, update language preference on server
    if (isAuthenticated) {
      try {
        await api.updateUserLanguage(newLocale);
      } catch (error) {
        console.error('Failed to update language preference on server:', error);
        // Continue with local language switch even if server update fails
      }
    }

    router.replace(pathname, { locale: newLocale as "zh-CN" | "zh-TW" | "zh-HK" | "en-US" | "en-GB" | "en-AU" | "ja" });
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="h-8 px-2">
          <FlagIcon locale={locale} className="h-4 w-4 mr-1" />
          <span className="hidden sm:inline">{localeShortNames[locale as keyof typeof localeShortNames]}</span>
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {routing.locales.map((loc) => (
          <DropdownMenuItem
            key={loc}
            onClick={() => switchLocale(loc)}
            className={`cursor-pointer ${
              loc === locale ? 'bg-accent' : ''
            }`}
          >
            <div className="flex items-center">
              <FlagIcon locale={loc} className="h-4 w-4 mr-2" />
              {localeNames[loc as keyof typeof localeNames]}
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}