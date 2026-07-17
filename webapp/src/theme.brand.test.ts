import { describe, it, expect } from 'vitest';
import { lightTheme, darkTheme } from './theme';
import { brandConfig } from './brands';
import { KAITU_BRAND } from './brands/kaitu';
import { OVERLEAP_BRAND } from './brands/overleap';

describe('MUI theme derives from brand tokens', () => {
  it('dark palette primary/secondary come from brandConfig.theme.dark', () => {
    expect(darkTheme.palette.primary.main).toBe(brandConfig.theme.dark.primary.main);
    expect(darkTheme.palette.primary.light).toBe(brandConfig.theme.dark.primary.light);
    expect(darkTheme.palette.primary.dark).toBe(brandConfig.theme.dark.primary.dark);
    expect(darkTheme.palette.secondary.main).toBe(brandConfig.theme.dark.secondary.main);
  });

  it('light palette primary/secondary come from brandConfig.theme.light', () => {
    expect(lightTheme.palette.primary.main).toBe(brandConfig.theme.light.primary.main);
    expect(lightTheme.palette.secondary.main).toBe(brandConfig.theme.light.secondary.main);
  });

  // describe.runIf, not an early `return`: a bare return makes the assertions
  // vanish under K2_BRAND=overleap while the test still reports green — a
  // hollow pass. Skipping is honest; the closed-gate case gets its own real
  // assertions below. (webapp/CLAUDE.md — brand-adaptive test rule.)
  describe.runIf(brandConfig.id === 'kaitu')('kaitu', () => {
    it('values are byte-identical to the pre-split palette (no visual regression)', () => {
      expect(darkTheme.palette.primary.main).toBe('#42A5F5');
      expect(lightTheme.palette.primary.main).toBe('#1565C0');
    });
  });

  describe.runIf(brandConfig.id === 'overleap')('overleap', () => {
    it('uses its own palette and never falls back to the kaitu blues', () => {
      expect(darkTheme.palette.primary.main).toBe(OVERLEAP_BRAND.theme.dark.primary.main);
      expect(lightTheme.palette.primary.main).toBe(OVERLEAP_BRAND.theme.light.primary.main);
      // Guards the failure mode this whole gate exists to prevent: a silent
      // fallback to the other brand's tokens would still satisfy the generic
      // "derives from brandConfig" assertions above.
      expect(darkTheme.palette.primary.main).not.toBe(KAITU_BRAND.theme.dark.primary.main);
      expect(lightTheme.palette.primary.main).not.toBe(KAITU_BRAND.theme.light.primary.main);
    });
  });
});
