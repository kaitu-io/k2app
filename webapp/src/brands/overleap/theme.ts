import type { BrandThemeTokens } from '../types';

/**
 * Theme palette: working values pending final design sign-off (see plan's
 * open questions) — distinct violet/teal family so a mis-branded build is
 * obvious.
 */
export const OVERLEAP_THEME: BrandThemeTokens = {
  light: {
    primary: { main: '#5E35B1', light: '#7E57C2', dark: '#4527A0' },
    secondary: { main: '#00897B', light: '#26A69A', dark: '#00695C' },
  },
  dark: {
    primary: { main: '#9575CD', light: '#B39DDB', dark: '#673AB7' },
    secondary: { main: '#4DB6AC', light: '#80CBC4', dark: '#26A69A' },
  },
};
