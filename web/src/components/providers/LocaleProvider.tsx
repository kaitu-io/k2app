"use client";

import { useEffect } from 'react';

interface LocaleProviderProps {
  locale: string;
  children: React.ReactNode;
}

/**
 * Client component that sets the lang attribute on the html element
 * based on the current locale from i18n routing
 */
export function LocaleProvider({ locale, children }: LocaleProviderProps) {
  useEffect(() => {
    // Set the lang attribute on the html element for accessibility and SEO
    document.documentElement.lang = locale;
  }, [locale]);

  return <>{children}</>;
}