import { describe, it, expect } from 'vitest';
import { lightTheme, darkTheme } from './theme';

/**
 * Top-anchored MUI Snackbars render in a Portal at document.body with
 * `position: fixed`, so they live in viewport space — OUTSIDE the Layout
 * container that carries `paddingTop: env(safe-area-inset-top)`. Without a
 * theme-level offset they paint over the iOS status bar / notch.
 *
 * These guards lock in the safe-area offset on every top anchor variant for
 * both palettes so the fix can't silently regress.
 */
const TOP_ANCHOR_SLOTS = [
  'anchorOriginTopCenter',
  'anchorOriginTopLeft',
  'anchorOriginTopRight',
] as const;

describe('theme — Snackbar safe-area-inset', () => {
  for (const [name, theme] of [
    ['lightTheme', lightTheme],
    ['darkTheme', darkTheme],
  ] as const) {
    describe(name, () => {
      for (const slot of TOP_ANCHOR_SLOTS) {
        it(`offsets ${slot} by safe-area-inset-top`, () => {
          const overrides =
            theme.components?.MuiSnackbar?.styleOverrides as
              | Record<string, { top?: string }>
              | undefined;
          const top = overrides?.[slot]?.top;
          expect(top, `${slot}.top should be defined`).toBeTruthy();
          expect(top).toContain('safe-area-inset-top');
        });
      }
    });
  }
});
