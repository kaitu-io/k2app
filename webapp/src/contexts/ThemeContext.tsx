import React, { createContext, useContext, useMemo, ReactNode } from 'react';
import { ThemeProvider as MuiThemeProvider, createTheme } from '@mui/material/styles';
import { CacheProvider } from '@emotion/react';
import createCache from '@emotion/cache';
import { prefixer } from 'stylis';
import rtlPlugin from 'stylis-plugin-rtl';
import { useTranslation } from 'react-i18next';
import { languageDirection } from '../i18n/i18n';
import { CssBaseline } from '@mui/material';
// lightTheme is retained intentionally — dark is forced at runtime, but the
// light palette stays in the bundle so we can re-enable the switcher later.
import { lightTheme, darkTheme } from '../theme';
import { useStatusBar } from '../hooks/useStatusBar';

type ThemeMode = 'light' | 'dark' | 'system';

interface ThemeContextType {
  themeMode: ThemeMode;
  toggleTheme: () => void;
  setThemeMode: (mode: ThemeMode) => void;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

// Keep the light palette reachable from the bundle so dead-code elimination
// doesn't strip it while the switcher is hidden.
void lightTheme;

// Right-to-left languages: the stylis plugin mirrors every physical property
// emotion emits (margin-left, left, text-align…), so components need no
// per-language styles. Separate caches because flipped and unflipped rules
// must not share class names.
const ltrCache = createCache({ key: 'css' });
const rtlCache = createCache({ key: 'css-rtl', stylisPlugins: [prefixer, rtlPlugin] });
const rtlDarkTheme = createTheme(darkTheme, { direction: 'rtl' });

export const ThemeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const themeMode: ThemeMode = 'dark';
  const setThemeMode = (_mode: ThemeMode) => {};
  const toggleTheme = () => {};

  useStatusBar({ isDark: true });

  // <html dir> itself is set by i18n.ts on languageChanged.
  const { i18n } = useTranslation();
  const rtl = languageDirection(i18n.language) === 'rtl';
  const value = useMemo(() => ({ themeMode, toggleTheme, setThemeMode }), []);

  return (
    <ThemeContext.Provider value={value}>
      <CacheProvider value={rtl ? rtlCache : ltrCache}>
        <MuiThemeProvider theme={rtl ? rtlDarkTheme : darkTheme}>
          <CssBaseline />
          {children}
        </MuiThemeProvider>
      </CacheProvider>
    </ThemeContext.Provider>
  );
};

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (context === undefined) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};