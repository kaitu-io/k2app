import type { BrandThemeTokens } from '../types';

/**
 * Overleap visual identity — spec docs/superpowers/specs/2026-09-04-overleap-independent-release-design.md §1.1.
 *
 * Contrast rule against the peer brand: connected = mint, idle = brand violet,
 * sans-serif, 12px radius. theme.brand.test.ts pins every value below — change
 * the colour AND the test together so the diff shows what moved.
 */
export const OVERLEAP_THEME: BrandThemeTokens = {
  typography: {
    fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
  },
  light: {
    primary: { main: '#5B3FE0', light: '#7C5CFF', dark: '#4527A0' },
    secondary: { main: '#14B8A6', light: '#2DD4BF', dark: '#0F766E' },
  },
  dark: {
    primary: { main: '#7C5CFF', light: '#9D85FF', dark: '#5B3FE0' },
    secondary: { main: '#2DD4BF', light: '#5EEAD4', dark: '#14B8A6' },
  },
  surface: {
    background: '#0B0E14',
    paper: '#141926',
    border: 'rgba(124, 92, 255, 0.18)',
    textPrimary: '#E6E8F0',
    textSecondary: '#9AA0B4',
    radius: 12,
  },
  semantic: {
    success: { main: '#34D399', light: '#6EE7B7', dark: '#059669' },
    warning: { main: '#FBBF24', light: '#FCD34D', dark: '#D97706' },
    error: { main: '#F87171', light: '#FCA5A5', dark: '#DC2626' },
  },
  status: {
    connected: {
      main: '#2DD4BF',
      gradient: 'linear-gradient(135deg, #2DD4BF 0%, #14B8A6 100%)',
      glow: 'rgba(45, 212, 191, 0.35)',
      glowStrong: 'rgba(45, 212, 191, 0.5)',
    },
    idle: {
      main: '#7C5CFF',
      gradient: 'linear-gradient(135deg, #7C5CFF 0%, #5B3FE0 100%)',
      glow: 'rgba(124, 92, 255, 0.3)',
      glowStrong: 'rgba(124, 92, 255, 0.5)',
    },
    dormant: {
      border: 'rgba(255, 255, 255, 0.10)',
      icon: 'rgba(255, 255, 255, 0.30)',
    },
  },
};
