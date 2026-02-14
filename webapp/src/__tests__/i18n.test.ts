import { describe, it, expect } from 'vitest';

// Test that i18n initializes and returns correct strings
describe('i18n', () => {
  it('returns Chinese string for known key', async () => {
    const i18n = (await import('../i18n')).default;
    await i18n.changeLanguage('zh-CN');
    expect(i18n.t('common:appName')).toBe('Kaitu.io 开途');
  });

  it('returns English string', async () => {
    const i18n = (await import('../i18n')).default;
    await i18n.changeLanguage('en-US');
    expect(i18n.t('common:appName')).toBe('Kaitu.io');
  });
});
