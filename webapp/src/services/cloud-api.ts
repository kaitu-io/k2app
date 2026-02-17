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

      // 5. Parse the JSON response
      const jsonResponse = await httpResponse.json() as SResponse<T>;

      // 6. Handle 401: try token refresh
      if (httpResponse.status === 401 || jsonResponse.code === 401) {
        return await this._handle401<T>(method, path, body);
      }

      // 7. Return the response as SResponse
      return jsonResponse;
    } catch (error) {
      // Network error
      return { code: -1, message: (error as Error).message || 'Network error' };
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
      // Get refresh token
      const refreshToken = await authService.getRefreshToken();

      // Resolve entry URL for refresh
      const entry = await resolveEntry();

      // Attempt token refresh
      const refreshResponse = await fetch(entry + '/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken }),
      });

      const refreshJson = await refreshResponse.json() as SResponse<{
        token: string;
        refreshToken: string;
      }>;

      // If refresh failed, clear auth
      if (refreshJson.code !== 0 || !refreshResponse.ok) {
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        return { code: 401, message: 'Unauthorized' } as SResponse<T>;
      }

      // Save new tokens
      const data = refreshJson.data!;
      await authService.setTokens({
        accessToken: data.token,
        refreshToken: data.refreshToken,
      });

      // Retry original request with new token
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${data.token}`,
      };

      const fetchOptions: RequestInit = {
        method,
        headers,
      };
      if (body !== undefined) {
        fetchOptions.body = JSON.stringify(body);
      }

      const retryResponse = await fetch(entry + path, fetchOptions);
      const retryJson = await retryResponse.json() as SResponse<T>;

      return retryJson;
    } catch (error) {
      // Refresh attempt failed entirely
      await authService.clearTokens();
      useAuthStore.setState({ isAuthenticated: false });
      return { code: 401, message: 'Unauthorized' } as SResponse<T>;
    }
  },
};
