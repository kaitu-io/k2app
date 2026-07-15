import { describe, it, expect } from 'vitest';
import { getDownloadLinks, getAndroidDownloadLinks } from '../constants';
import { KAITU, OVERLEAP } from '../brands';

describe('brand-parameterized download links', () => {
  it('kaitu artifacts keep the exact legacy URLs', () => {
    const links = getDownloadLinks('0.5.0', KAITU);
    expect(links.windows.primary).toBe('https://dl.kaitu.io/kaitu/desktop/0.5.0/Kaitu_0.5.0_x64.exe');
    expect(links.macos.backup).toBe('https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.5.0/Kaitu_0.5.0_universal.pkg');
    expect(getAndroidDownloadLinks('0.5.0', KAITU).primary).toBe('https://dl.kaitu.io/kaitu/android/0.5.0/Kaitu-0.5.0.apk');
  });
  it('overleap artifacts use /overleap/ CDN layout and Overleap_ prefix (spec §8)', () => {
    const links = getDownloadLinks('0.5.0', OVERLEAP);
    expect(links.windows.primary).toBe('https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/0.5.0/Overleap_0.5.0_x64.exe');
    expect(getAndroidDownloadLinks('0.5.0', OVERLEAP).primary).toBe('https://d13jc1jqzlg4yt.cloudfront.net/overleap/android/0.5.0/Overleap-0.5.0.apk');
    expect(JSON.stringify(links)).not.toContain('kaitu');
  });
  it('single-base brands fall back backup=primary', () => {
    const links = getDownloadLinks('0.5.0', OVERLEAP);
    expect(links.windows.backup).toBe(links.windows.primary);
  });
});
