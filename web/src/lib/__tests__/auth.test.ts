/**
 * Auth Utilities Tests
 */
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { redirectToLogin } from '../auth';

describe('Auth Utilities', () => {
  // Save original window.location
  const originalLocation = window.location;

  beforeEach(() => {
    // Mock window.location
    delete (window as { location?: Location }).location;
    window.location = {
      ...originalLocation,
      href: '',
      pathname: '/dashboard',
      search: '',
    } as Location;
  });

  afterEach(() => {
    window.location = originalLocation;
  });

  describe('redirectToLogin', () => {
    it('should redirect to login page with current path', () => {
      window.location.pathname = '/dashboard';
      window.location.search = '';

      redirectToLogin();

      expect(window.location.href).toBe('/login?next=%2Fdashboard');
    });

    it('should redirect with custom path', () => {
      redirectToLogin('/custom-page');

      expect(window.location.href).toBe('/login?next=%2Fcustom-page');
    });

    it('should preserve query parameters', () => {
      window.location.pathname = '/dashboard';
      window.location.search = '?filter=active';

      redirectToLogin();

      expect(window.location.href).toBe('/login?next=%2Fdashboard%3Ffilter%3Dactive');
    });

    it('should remove locale prefix from path', () => {
      redirectToLogin('/zh-CN/manager/orders');

      expect(window.location.href).toBe('/login?next=%2Fmanager%2Forders');
    });

    it('should handle en-US locale', () => {
      redirectToLogin('/en-US/dashboard');

      expect(window.location.href).toBe('/login?next=%2Fdashboard');
    });

    it('should handle ja locale', () => {
      redirectToLogin('/ja/settings');

      expect(window.location.href).toBe('/login?next=%2Fsettings');
    });

    it('should handle paths without locale prefix', () => {
      redirectToLogin('/settings');

      expect(window.location.href).toBe('/login?next=%2Fsettings');
    });

    it('should handle root path', () => {
      redirectToLogin('/');

      expect(window.location.href).toBe('/login?next=%2F');
    });

    it('should handle locale-only path', () => {
      redirectToLogin('/zh-CN');

      expect(window.location.href).toBe('/login?next=%2F');
    });
  });

  describe('Supported Locales', () => {
    const supportedLocales = ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'];

    supportedLocales.forEach((locale) => {
      it(`should remove ${locale} prefix`, () => {
        redirectToLogin(`/${locale}/test-page`);

        expect(window.location.href).toBe('/login?next=%2Ftest-page');
      });
    });

    it('should not remove unsupported locale-like prefixes', () => {
      redirectToLogin('/fr-FR/test-page');

      // fr-FR is not in supported locales, so it should be preserved
      expect(window.location.href).toBe('/login?next=%2Ffr-FR%2Ftest-page');
    });
  });

  describe('Edge Cases', () => {
    it('should handle deeply nested paths', () => {
      redirectToLogin('/zh-CN/manager/users/123/devices');

      expect(window.location.href).toBe('/login?next=%2Fmanager%2Fusers%2F123%2Fdevices');
    });

    it('should handle special characters in path', () => {
      redirectToLogin('/search?q=hello%20world');

      expect(window.location.href).toBe('/login?next=%2Fsearch%3Fq%3Dhello%2520world');
    });

    it('should handle empty string path', () => {
      window.location.pathname = '';
      window.location.search = '';

      redirectToLogin();

      expect(window.location.href).toBe('/login?next=');
    });
  });
});
