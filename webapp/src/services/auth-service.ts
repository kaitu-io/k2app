/**
 * Auth Service - Token and UDID Management
 *
 * Manages authentication tokens and device UDID using platform secure storage.
 * This replaces the Go service's internal token management.
 *
 * Storage Keys:
 * - k2.auth.token: Access token (encrypted in secure storage)
 * - k2.auth.refresh: Refresh token (encrypted in secure storage)
 * - k2.udid: Device UDID (managed by platform.getUdid())
 *
 * Usage:
 * ```typescript
 * import { authService } from './auth-service';
 *
 * // Get credentials for tunnel URL
 * const { udid, token } = await authService.getCredentials();
 *
 * // Save token after login
 * await authService.setTokens({ accessToken: '...', refreshToken: '...' });
 *
 * // Build authenticated tunnel URL
 * const url = await authService.buildTunnelUrl('k2v4://example.com?ipv4=1.2.3.4');
 * // Result: 'k2v4://udid:token@example.com?ipv4=1.2.3.4'
 * ```
 */

// Storage keys
export const TOKEN_STORAGE_KEY = 'k2.auth.token';
export const REFRESH_TOKEN_STORAGE_KEY = 'k2.auth.refresh';

/**
 * Token pair returned from login/register/refresh
 */
export interface TokenPair {
  accessToken: string;
  refreshToken?: string;
}

/**
 * Credentials for tunnel authentication
 */
export interface TunnelCredentials {
  udid: string;
  token: string | null;
}

/**
 * Get the platform instance
 * Uses window._platform which is injected by Tauri/Capacitor/Web
 */
function getPlatform() {
  if (!window._platform) {
    throw new Error('[AuthService] Platform not available');
  }
  return window._platform;
}

/**
 * Get secure storage from platform
 */
function getStorage() {
  const platform = getPlatform();
  if (!platform.storage) {
    throw new Error('[AuthService] Secure storage not available');
  }
  return platform.storage;
}

/**
 * Auth Service
 */
export const authService = {
  /**
   * Get current access token
   * @returns Access token or null if not logged in
   */
  async getToken(): Promise<string | null> {
    try {
      const storage = getStorage();
      return await storage.get(TOKEN_STORAGE_KEY);
    } catch (error) {
      console.warn('[AuthService] Failed to get token:', error);
      return null;
    }
  },

  /**
   * Get current refresh token
   * @returns Refresh token or null if not available
   */
  async getRefreshToken(): Promise<string | null> {
    try {
      const storage = getStorage();
      return await storage.get(REFRESH_TOKEN_STORAGE_KEY);
    } catch (error) {
      console.warn('[AuthService] Failed to get refresh token:', error);
      return null;
    }
  },

  /**
   * Save tokens after login/register/refresh
   * @param tokens Token pair from API response
   */
  async setTokens(tokens: TokenPair): Promise<void> {
    const storage = getStorage();

    // Save access token
    await storage.set(TOKEN_STORAGE_KEY, tokens.accessToken);
    console.debug('[AuthService] Access token saved');

    // Save refresh token if provided
    if (tokens.refreshToken) {
      await storage.set(REFRESH_TOKEN_STORAGE_KEY, tokens.refreshToken);
      console.debug('[AuthService] Refresh token saved');
    }
  },

  /**
   * Clear all tokens (on logout or 401)
   */
  async clearTokens(): Promise<void> {
    const storage = getStorage();

    try {
      await storage.remove(TOKEN_STORAGE_KEY);
      await storage.remove(REFRESH_TOKEN_STORAGE_KEY);
      console.debug('[AuthService] Tokens cleared');
    } catch (error) {
      console.warn('[AuthService] Failed to clear tokens:', error);
    }
  },

  /**
   * Get device UDID
   * Uses platform.getUdid() which handles generation and caching
   * @returns 57-character UDID
   */
  async getUdid(): Promise<string> {
    const platform = getPlatform();

    if (!platform.getUdid) {
      throw new Error('[AuthService] Platform does not support getUdid()');
    }

    return platform.getUdid();
  },

  /**
   * Get credentials for tunnel authentication
   * @returns UDID and token (token may be null if not logged in)
   */
  async getCredentials(): Promise<TunnelCredentials> {
    const [udid, token] = await Promise.all([
      this.getUdid(),
      this.getToken(),
    ]);

    return { udid, token };
  },

  /**
   * Check if user has a valid token
   * Note: This only checks if a token exists, not if it's expired
   * @returns true if token exists
   */
  async hasToken(): Promise<boolean> {
    const token = await this.getToken();
    return token !== null && token.length > 0;
  },

};
