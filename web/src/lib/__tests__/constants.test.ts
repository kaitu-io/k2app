import { describe, it, expect } from 'vitest';
import { getDownloadLinks, getAndroidDownloadLinks } from '../constants';
import { KAITU, type Brand } from '../brands';

describe('brand-parameterized download links', () => {
  it('kaitu artifacts keep the exact legacy URLs', () => {
    const links = getDownloadLinks('0.5.0', KAITU);
    expect(links.windows.primary).toBe('https://dl.kaitu.io/kaitu/desktop/0.5.0/Kaitu_0.5.0_x64.exe');
    expect(links.macos.backup).toBe('https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.5.0/Kaitu_0.5.0_universal.pkg');
    expect(getAndroidDownloadLinks('0.5.0', KAITU).primary).toBe('https://dl.kaitu.io/kaitu/android/0.5.0/Kaitu-0.5.0.apk');
  });
  it('a single CDN base falls back backup=primary', () => {
    const singleBase: Brand = {
      ...KAITU,
      cdn: { ...KAITU.cdn, desktopBases: [KAITU.cdn.desktopBases[0]], mobileBases: [KAITU.cdn.mobileBases[0]] },
    };
    const links = getDownloadLinks('0.5.0', singleBase);
    expect(links.windows.backup).toBe(links.windows.primary);
    const android = getAndroidDownloadLinks('0.5.0', singleBase);
    expect(android.backup).toBe(android.primary);
  });
});
