import React, { createContext, useContext, ReactNode } from 'react';
import { ThemeProvider as MuiThemeProvider } from '@mui/material/styles';
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

export const ThemeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const themeMode: ThemeMode = 'dark';
  const setThemeMode = (_mode: ThemeMode) => {};
  const toggleTheme = () => {};

  useStatusBar({ isDark: true });

  return (
    <ThemeContext.Provider value={{ themeMode, toggleTheme, setThemeMode }}>
      <MuiThemeProvider theme={darkTheme}>
        <CssBaseline />
        {children}
      </MuiThemeProvider>
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