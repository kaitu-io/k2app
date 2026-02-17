/**
 * Auth Service v2 Tests (F2 - RED phase)
 *
 * Tests for auth-service migrated from window._k2.platform to window._platform.
 *
 * Key change: auth-service should use window._platform.storage and
 * window._platform.getUdid() instead of window._k2.platform.
 *
 * Run: cd webapp && npx vitest run --reporter=verbose src/services/__tests__/auth-service-v2.test.ts
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { TOKEN_STORAGE_KEY, REFRESH_TOKEN_STORAGE_KEY } from '../auth-service';

// Mock storage (implements ISecureStorage interface)
const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  clear: vi.fn(),
  keys: vi.fn(),
};

// Mock _platform (implements IPlatform interface)
const mockPlatform = {
  os: 'macos' as const,
  isDesktop: true,
  isMobile: false,
  version: '0.4.0',
  storage: mockStorage,
  getUdid: vi.fn(),
};

describe('Auth Service v2 (using window._platform)', () => {
  beforeEach(() => {
    // Set up window._platform (the new way)
    // Do NOT set window._k2.platform -- auth-service should not use it
    (window as any)._platform = mockPlatform;

    // Ensure window._k2 exists but has NO platform property
    // This verifies auth-service uses _platform, not _k2.platform
    (window as any)._k2 = {
      run: vi.fn(),
      // Deliberately no `platform` property
    };

    vi.clearAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    delete (window as any)._platform;
    delete (window as any)._k2;
  });

  // ==================== Token via _platform.storage ====================

  describe('getToken', () => {
    it('should read token from window._platform.storage', async () => {
      mockStorage.get.mockResolvedValue('platform-token-v2');

      const { authService } = await import('../auth-service');
      const token = await authService.getToken();

      expect(token).toBe('platform-token-v2');
      expect(mockStorage.get).toHaveBeenCalledWith(TOKEN_STORAGE_KEY);
    });

    it('should return null when no token in storage', async () => {
      mockStorage.get.mockResolvedValue(null);

      const { authService } = await import('../auth-service');
      const token = await authService.getToken();

      expect(token).toBeNull();
    });
  });

  describe('setTokens', () => {
    it('should write tokens to window._platform.storage', async () => {
      mockStorage.set.mockResolvedValue(undefined);

      const { authService } = await import('../auth-service');
      await authService.setTokens({
        accessToken: 'new-access-token',
        refreshToken: 'new-refresh-token',
      });

      expect(mockStorage.set).toHaveBeenCalledWith(TOKEN_STORAGE_KEY, 'new-access-token');
      expect(mockStorage.set).toHaveBeenCalledWith(REFRESH_TOKEN_STORAGE_KEY, 'new-refresh-token');
    });

    it('should only write access token when no refresh token provided', async () => {
      mockStorage.set.mockResolvedValue(undefined);

      const { authService } = await import('../auth-service');
      await authService.setTokens({ accessToken: 'only-access' });

      expect(mockStorage.set).toHaveBeenCalledWith(TOKEN_STORAGE_KEY, 'only-access');
      expect(mockStorage.set).not.toHaveBeenCalledWith(
        REFRESH_TOKEN_STORAGE_KEY,
        expect.anything()
      );
    });
  });

  // ==================== UDID via _platform.getUdid() ====================

  describe('getUdid', () => {
    it('should call window._platform.getUdid()', async () => {
      const expectedUdid = 'a'.repeat(48) + '-' + 'b'.repeat(8);
      mockPlatform.getUdid.mockResolvedValue(expectedUdid);

      const { authService } = await import('../auth-service');
      const udid = await authService.getUdid();

      expect(udid).toBe(expectedUdid);
      expect(mockPlatform.getUdid).toHaveBeenCalled();
    });

    it('should throw if window._platform is undefined', async () => {
      delete (window as any)._platform;

      const { authService } = await import('../auth-service');

      await expect(authService.getUdid()).rejects.toThrow();
    });
  });

  // ==================== clearTokens via _platform.storage ====================

  describe('clearTokens', () => {
    it('should remove both tokens from window._platform.storage', async () => {
      mockStorage.remove.mockResolvedValue(undefined);

      const { authService } = await import('../auth-service');
      await authService.clearTokens();

      expect(mockStorage.remove).toHaveBeenCalledWith(TOKEN_STORAGE_KEY);
      expect(mockStorage.remove).toHaveBeenCalledWith(REFRESH_TOKEN_STORAGE_KEY);
    });
  });

  // ==================== Verify NOT using _k2.platform ====================

  describe('platform source verification', () => {
    it('should NOT access window._k2.platform for storage', async () => {
      // Set up a spy on _k2 to detect if platform is accessed
      const k2PlatformAccess = vi.fn();
      Object.defineProperty((window as any)._k2, 'platform', {
        get: k2PlatformAccess,
        configurable: true,
      });

      mockStorage.get.mockResolvedValue('test-token');

      const { authService } = await import('../auth-service');
      await authService.getToken();

      // Should NOT have accessed window._k2.platform
      expect(k2PlatformAccess).not.toHaveBeenCalled();
    });
  });
});
