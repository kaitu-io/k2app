// web/tests/downloads.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock fetch globally
const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

// Mock constants
vi.mock('@/lib/constants', () => ({
  CDN_PRIMARY: 'https://cdn-primary.test/kaitu/desktop',
  CDN_BACKUP: 'https://cdn-backup.test/kaitu/desktop',
  CDN_BASE_PRIMARY: 'https://cdn-primary.test/kaitu',
  CDN_BASE_BACKUP: 'https://cdn-backup.test/kaitu',
  getDownloadLinks: (version: string) => ({
    windows: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_x64.exe`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_x64.exe`,
    },
    macos: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_universal.pkg`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_universal.pkg`,
    },
    linux: {
      primary: `https://cdn-primary.test/kaitu/desktop/${version}/Kaitu_${version}_amd64.AppImage`,
      backup: `https://cdn-backup.test/kaitu/desktop/${version}/Kaitu_${version}_amd64.AppImage`,
    },
  }),
  getAndroidDownloadLinks: (version: string) => ({
    primary: `https://dl.kaitu.io/kaitu/android/${version}/Kaitu-${version}.apk`,
    backup: `https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/${version}/Kaitu-${version}.apk`,
  }),
}));

describe('fetchAllDownloadLinks', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('fetches desktop beta + stable versions and mobile links', async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/beta/cloudfront.latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0-beta.1' }) });
      }
      if (url.includes('/cloudfront.latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.3.22' }) });
      }
      if (url.includes('/ios/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ appstore_url: 'https://apps.apple.com/app/id6759199298' }) });
      }
      if (url.includes('/android/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0', url: 'https://d0.all7.cc/kaitu/android/0.4.0/Kaitu-0.4.0.apk' }) });
      }
      return Promise.resolve({ ok: false });
    });

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).not.toBeNull();
    expect(result.desktop.beta!.version).toBe('0.4.0-beta.1');
    expect(result.desktop.stable).not.toBeNull();
    expect(result.desktop.stable!.version).toBe('0.3.22');
    expect(result.mobile).not.toBeNull();
    expect(result.mobile!.ios).toBe('https://apps.apple.com/app/id6759199298');
    expect(result.mobile!.android.primary).toBe('https://dl.kaitu.io/kaitu/android/0.4.0/Kaitu-0.4.0.apk');
    expect(result.mobile!.android.backup).toBe('https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/0.4.0/Kaitu-0.4.0.apk');
  });

  it('returns null for desktop channels when CDN is unreachable', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'));

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).toBeNull();
    expect(result.desktop.stable).toBeNull();
    expect(result.mobile).toBeNull();
  });

  it('falls back to backup CDN when primary fails', async () => {
    mockFetch.mockImplementation((url: string) => {
      // Primary fails for desktop
      if (url.startsWith('https://cdn-primary.test') && url.includes('cloudfront.latest.json')) {
        return Promise.resolve({ ok: false });
      }
      // Backup succeeds for desktop
      if (url.startsWith('https://cdn-backup.test') && url.includes('cloudfront.latest.json')) {
        if (url.includes('/beta/')) return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0-beta.1' }) });
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.3.22' }) });
      }
      // Primary fails for mobile
      if (url.startsWith('https://dl.kaitu.io')) {
        return Promise.resolve({ ok: false });
      }
      // Backup succeeds for mobile
      if (url.includes('/ios/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ appstore_url: 'https://apps.apple.com/app/test' }) });
      }
      if (url.includes('/android/latest.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ version: '0.4.0', url: 'https://cdn/android.apk' }) });
      }
      return Promise.resolve({ ok: false });
    });

    const { fetchAllDownloadLinks } = await import('../src/lib/downloads');
    const result = await fetchAllDownloadLinks();

    expect(result.desktop.beta).not.toBeNull();
    expect(result.desktop.stable).not.toBeNull();
  });
});

describe('flattenToRecord', () => {
  it('flattens AllDownloadLinks to Record<string, string> for device-detection compat', async () => {
    const { flattenToRecord } = await import('../src/lib/downloads');

    const result = flattenToRecord({
      desktop: {
        beta: {
          version: '0.4.0-beta.1',
          links: {
            windows: { primary: 'https://cdn/win.exe', backup: 'https://backup/win.exe' },
            macos: { primary: 'https://cdn/mac.pkg', backup: 'https://backup/mac.pkg' },
            linux: { primary: 'https://cdn/linux.AppImage', backup: 'https://backup/linux.AppImage' },
          },
        },
        stable: null,
      },
      mobile: {
        ios: 'https://apps.apple.com/app/id123',
        android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' },
      },
    });

    expect(result).toEqual({
      windows: 'https://cdn/win.exe',
      macos: 'https://cdn/mac.pkg',
      ios: 'https://apps.apple.com/app/id123',
      android: 'https://cdn/android.apk',
    });
  });

  it('returns empty strings when no data available', async () => {
    const { flattenToRecord } = await import('../src/lib/downloads');

    const result = flattenToRecord({
      desktop: { beta: null, stable: null },
      mobile: null,
    });

    expect(result).toEqual({ windows: '', macos: '', ios: '', android: '' });
  });
});
