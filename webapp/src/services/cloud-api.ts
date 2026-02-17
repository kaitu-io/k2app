/**
 * Cloud API Client
 *
 * Replaces window._k2.api with direct HTTP fetch() calls.
 *
 * Responsibilities:
 * - Base URL resolution (relative URLs for now, antiblock later)
 * - Auth header injection: Bearer token from authService.getToken()
 * - 401 handling: refresh token, retry once, then logout
 * - Standard fetch() wrapper returning SResponse format
 */

import type { SResponse } from '../types/kaitu-core';
import { authService } from './auth-service';
import { useAuthStore } from '../stores/auth.store';
import { resolveEntry } from './antiblock';
import { cacheStore } from './cache-store';

/** Auth paths where tokens should be auto-saved on success */
const AUTH_TOKEN_PATHS = ['/api/auth/login', '/api/auth/register', '/api/auth/refresh'];

/** Auth path where tokens + cache should be cleared on success */
const AUTH_LOGOUT_PATH = '/api/auth/logout';

/** Module-level refresh promise for concurrent 401 dedup */
let _refreshPromise: Promise<boolean> | null = null;

/**
 * Cloud API client for direct HTTP communication with the cloud API.
 *
 * Replaces the old window._k2.api pattern
 * with cloudApi.request(method, path, body).
 */
export const cloudApi = {
  /**
   * Make an HTTP request to the cloud API.
   *
   * @param method - HTTP method (GET, POST, PUT, DELETE)
   * @param path - API path (e.g., '/api/user/info')
   * @param body - Request body for POST/PUT (will be JSON-serialized)
   * @returns SResponse format: { code, data?, message? }
   */
  async request<T = any>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<SResponse<T>> {
    try {
      // 1. Get token
      const token = await authService.getToken();

      // 2. Build headers
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }

      // 3. Build fetch options
      const fetchOptions: RequestInit = {
        method,
        headers,
      };
      if (body !== undefined) {
        fetchOptions.body = JSON.stringify(body);
      }

      // 4. Resolve entry URL via antiblock
      const entry = await resolveEntry();

      // 5. Make the request
      const httpResponse = await fetch(entry + path, fetchOptions);

      // 6. Parse the JSON response
      const jsonResponse = await httpResponse.json() as SResponse<T>;

      // 7. Handle 401: try token refresh
      if (httpResponse.status === 401 || jsonResponse.code === 401) {
        return await this._handle401<T>(method, path, body);
      }

      // 8. Auto-handle auth paths on success
      if (jsonResponse.code === 0) {
        await this._handleAuthPath(path, jsonResponse);
      }

      // 9. Return the response as SResponse
      return jsonResponse;
    } catch (error) {
      // Network error
      return { code: -1, message: (error as Error).message || 'Network error' };
    }
  },

  /**
   * Convenience GET request.
   */
  async get<T = any>(path: string): Promise<SResponse<T>> {
    return this.request<T>('GET', path);
  },

  /**
   * Convenience POST request.
   */
  async post<T = any>(path: string, body?: unknown): Promise<SResponse<T>> {
    return this.request<T>('POST', path, body);
  },

  /**
   * Auto-save tokens on auth login/register/refresh paths,
   * or clear tokens + cache on logout path.
   */
  async _handleAuthPath<T>(path: string, response: SResponse<T>): Promise<void> {
    if (AUTH_TOKEN_PATHS.includes(path)) {
      const data = response.data as Record<string, unknown> | undefined;
      if (data) {
        const accessToken = (data.accessToken ?? data.token) as string | undefined;
        const refreshToken = data.refreshToken as string | undefined;
        if (accessToken) {
          await authService.setTokens({
            accessToken,
            refreshToken,
          });
        }
      }
    } else if (path === AUTH_LOGOUT_PATH) {
      await authService.clearTokens();
      cacheStore.clear();
    }
  },

  /**
   * Handle 401 by refreshing the token and retrying the original request.
   * If refresh fails, clear tokens and set isAuthenticated = false.
   */
  async _handle401<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<SResponse<T>> {
    try {
      // Get refresh token — if null, skip refresh entirely
      const refreshToken = await authService.getRefreshToken();
      if (!refreshToken) {
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        return { code: 401, message: 'No refresh token' } as SResponse<T>;
      }

      // Concurrent 401 dedup: share a single refresh promise
      if (!_refreshPromise) {
        _refreshPromise = this._doRefresh(refreshToken)
          .finally(() => { _refreshPromise = null; });
      }
      const success = await _refreshPromise;

      if (!success) {
        return { code: 401, message: 'Unauthorized' } as SResponse<T>;
      }

      // Retry original request — request() auto-injects new token from storage
      return await this.request<T>(method, path, body);
    } catch (error) {
      // Refresh attempt failed entirely
      await authService.clearTokens();
      useAuthStore.setState({ isAuthenticated: false });
      return { code: 401, message: 'Unauthorized' } as SResponse<T>;
    }
  },

  /**
   * Perform the actual token refresh HTTP call.
   * @returns true if refresh succeeded, false otherwise
   */
  async _doRefresh(refreshToken: string): Promise<boolean> {
    try {
      const entry = await resolveEntry();

      const refreshResponse = await fetch(entry + '/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken }),
      });

      const refreshJson = await refreshResponse.json() as SResponse<{
        token: string;
        refreshToken: string;
      }>;

      if (refreshJson.code !== 0 || !refreshResponse.ok) {
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        return false;
      }

      // Save new tokens
      const data = refreshJson.data!;
      await authService.setTokens({
        accessToken: data.token,
        refreshToken: data.refreshToken,
      });

      return true;
    } catch {
      await authService.clearTokens();
      useAuthStore.setState({ isAuthenticated: false });
      return false;
    }
  },
};
