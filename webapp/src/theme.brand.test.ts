import { describe, it, expect } from 'vitest';
import { lightTheme, darkTheme } from './theme';
import { brandConfig } from './brand';

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

  it('kaitu values are byte-identical to the pre-split palette (no visual regression)', () => {
    if (brandConfig.id !== 'kaitu') return;
    expect(darkTheme.palette.primary.main).toBe('#42A5F5');
    expect(lightTheme.palette.primary.main).toBe('#1565C0');
  });
});
