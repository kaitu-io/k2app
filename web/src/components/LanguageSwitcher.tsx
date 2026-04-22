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
import { ownerBrand, brandById, type BrandId } from '@/lib/brands';
import { isProductionHost } from '@/lib/host-utils';
import { useCurrentBrand } from '@/hooks/useCurrentBrand';

/**
 * Pure switching-decision function: given the target locale and current context,
 * decides whether the locale change can be done in-place (same-host router replace)
 * or requires a full cross-domain navigation to the owning brand's site.
 *
 * Cross-brand navigation only triggers on production hosts — preview/localhost
 * always stay in-place so developers can exercise every locale without bouncing
 * between kaitu.io and overleap.io.
 *
 * Kept as a named export so it is unit-testable without Radix dropdown plumbing.
 */
export type SwitchAction =
  | { type: 'router'; pathname: string; locale: string }
  | { type: 'assign'; url: string };

export function computeSwitchAction(input: {
  newLocale: string;
  pathname: string;
  currentBrand: BrandId;
  currentHost: string;
  search: string;
  hash: string;
}): SwitchAction {
  const { newLocale, pathname, currentBrand, currentHost, search, hash } = input;
  const targetBrand = ownerBrand(newLocale);
  if (targetBrand === currentBrand || !isProductionHost(currentHost)) {
    return { type: 'router', pathname, locale: newLocale };
  }
  const target = brandById(targetBrand);
  // target.baseUrl already includes https:// — compose the full external URL.
  const url = `${target.baseUrl}/${newLocale}${pathname}${search}${hash}`;
  return { type: 'assign', url };
}

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
  const currentBrand = useCurrentBrand();

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

    // Decide in-place router nav vs cross-domain full navigation.
    const action = computeSwitchAction({
      newLocale,
      pathname,
      currentBrand,
      currentHost: typeof window !== 'undefined' ? window.location.host : '',
      search: typeof window !== 'undefined' ? window.location.search : '',
      hash: typeof window !== 'undefined' ? window.location.hash : '',
    });

    if (action.type === 'assign') {
      window.location.assign(action.url);
      return;
    }
    router.replace(action.pathname, {
      locale: action.locale as 'zh-CN' | 'zh-TW' | 'zh-HK' | 'en-US' | 'en-GB' | 'en-AU' | 'ja',
    });
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
              loc === locale ? 'bg-accent text-accent-foreground' : ''
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