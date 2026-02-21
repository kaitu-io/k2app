"use client";

import React, { useEffect } from 'react';
import { ThemeProvider, useTheme } from 'next-themes';
import { useEmbedMode } from '@/hooks/useEmbedMode';

interface EmbedThemeProviderProps {
  children: React.ReactNode;
}

/**
 * Internal theme controller that manages embed theme using next-themes
 * This component runs inside ThemeProvider and can access useTheme hook
 */
function EmbedThemeController({ children }: { children: React.ReactNode }) {
  const { isEmbedded, embedTheme } = useEmbedMode();
  const { setTheme, theme, systemTheme } = useTheme();

  // Effect to apply embed theme when in embed mode using next-themes API
  useEffect(() => {
    if (!isEmbedded || !embedTheme) return;

    let targetTheme: string;

    switch (embedTheme) {
      case 'dark':
        targetTheme = 'dark';
        console.log('[EmbedThemeProvider] Setting theme to dark');
        break;
      case 'light':
        targetTheme = 'light';
        console.log('[EmbedThemeProvider] Setting theme to light');
        break;
      case 'auto':
      default:
        targetTheme = 'system';
        console.log('[EmbedThemeProvider] Setting theme to system');
        break;
    }

    // Only update if different to avoid unnecessary re-renders
    if (theme !== targetTheme) {
      setTheme(targetTheme);
      console.log('[EmbedThemeProvider] Theme updated from', theme, 'to', targetTheme);
    }
  }, [isEmbedded, embedTheme, theme, setTheme]);

  // Effect to add embed-mode class to body when in embed mode
  useEffect(() => {
    if (isEmbedded) {
      document.body.classList.add('embed-mode');
      console.log('[EmbedThemeProvider] Added embed-mode class to body');
    } else {
      document.body.classList.remove('embed-mode');
    }

    // Cleanup function to remove class when component unmounts
    return () => {
      document.body.classList.remove('embed-mode');
    };
  }, [isEmbedded]);

  // Log theme changes for debugging
  useEffect(() => {
    if (isEmbedded) {
      const resolvedTheme = theme === 'system' ? systemTheme : theme;
      console.log('[EmbedThemeProvider] Current theme:', theme, 'Resolved:', resolvedTheme);
    }
  }, [isEmbedded, theme, systemTheme]);

  return <>{children}</>;
}

/**
 * Theme provider that handles both regular theme preferences and embed-specific theme parameters
 * In embed mode, theme parameter takes precedence over user preferences
 * Supports: auto (system), light, dark themes
 * 
 * This implementation uses next-themes API instead of direct DOM manipulation
 * for better React integration and proper state management
 */
export function EmbedThemeProvider({ children }: EmbedThemeProviderProps) {
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="dark"
      disableTransitionOnChange
    >
      <EmbedThemeController>
        {children}
      </EmbedThemeController>
    </ThemeProvider>
  );
}