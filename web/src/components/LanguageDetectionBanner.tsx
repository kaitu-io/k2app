'use client';

import { useState, useEffect } from 'react';
import { Button } from '@/components/ui/button';
import { X } from 'lucide-react';
import { useRouter, usePathname } from '@/i18n/routing';
import { useLocale, useTranslations } from 'next-intl';
import Cookies from 'js-cookie';
import FlagIcon from './FlagIcon';

const localeNames: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文 (台灣)',
  'zh-HK': '繁體中文 (香港)',
  'en-US': 'English (US)',
  'en-GB': 'English (UK)',
  'en-AU': 'English (AU)',
  'ja': '日本語'
};

export default function LanguageDetectionBanner() {
  const [showBanner, setShowBanner] = useState(false);
  const [suggestedLocale, setSuggestedLocale] = useState<string | null>(null);
  const router = useRouter();
  const pathname = usePathname();
  const currentLocale = useLocale();
  const t = useTranslations();
  
  useEffect(() => {
    // Check if this is a first visit and if browser language differs from current
    const hasVisited = Cookies.get('hasVisited');
    const preferredLocale = Cookies.get('preferredLocale');
    const suggested = Cookies.get('suggestedLocale');
    
    // Don't show banner if user has already set a preference
    if (preferredLocale) {
      setShowBanner(false);
      return;
    }
    
    // Show banner if suggested locale is different from current locale
    if (suggested && suggested !== currentLocale && !hasVisited) {
      setSuggestedLocale(suggested);
      setShowBanner(true);
      // Mark as visited after showing the banner
      Cookies.set('hasVisited', 'true', { expires: 365 });
    }
  }, [currentLocale]);
  
  const handleAccept = () => {
    if (suggestedLocale) {
      // Save preference
      Cookies.set('preferredLocale', suggestedLocale, { expires: 365 });
      // Navigate to suggested locale
      router.push(pathname, { locale: suggestedLocale as "zh-CN" | "zh-TW" | "zh-HK" | "en-US" | "en-GB" | "en-AU" | "ja" });
    }
    setShowBanner(false);
  };
  
  const handleDismiss = () => {
    // Save current locale as preference
    Cookies.set('preferredLocale', currentLocale, { expires: 365 });
    setShowBanner(false);
  };
  
  if (!showBanner || !suggestedLocale) {
    return null;
  }
  
  return (
    <div className="fixed top-16 left-0 right-0 z-40 bg-accent/10 border-b border-accent/20 p-3">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between">
        <div className="flex items-center space-x-3">
          <FlagIcon locale={suggestedLocale} className="h-5 w-5" />
          <span className="text-sm text-accent-foreground">
            {t('nav.languageDetection.message')} <strong>{localeNames[suggestedLocale]}</strong>{t('nav.languageDetection.question')}
          </span>
        </div>
        <div className="flex items-center space-x-2">
          <Button
            size="sm"
            variant="default"
            onClick={handleAccept}
          >
            <FlagIcon locale={suggestedLocale} className="h-4 w-4 mr-1" />
            {t('nav.languageDetection.switchTo')} {localeNames[suggestedLocale]}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={handleDismiss}
            className="text-primary hover:text-primary/80"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}