/**
 * Cloud API Client
 *
 * Replaces window._k2.api with direct HTTP fetch() calls.
 *
 * Responsibilities:
 * - Transport resolution: camouflage-node relay → direct fallback (via resolveAndFetch)
 * - Auth header injection: Bearer token from authService.getToken()
 * - 401 handling: refresh token, retry once, then logout
 * - Standard fetch() wrapper returning SResponse format
 * - Entry pool seeding: /api/tunnels responses populate relay node pool
 */

import type { SResponse } from '../types/kaitu-core';
import { authService } from './auth-service';
import { useAuthStore } from '../stores/auth.store';
import { resolveAndFetch } from './resolve-and-fetch';
import { nodeEntriesFromTunnels } from './node-descriptor';
import { addNodes } from './entry-pool';
import { cacheStore } from './cache-store';
import { useLoginDialogStore } from '../stores/login-dialog.store';
import i18n from '../i18n/i18n';
import { getBrandId } from '../brand';

/**
 * Build X-K2-Client header — sole origination point for this header.
 *
 * Format: RFC 7231 User-Agent grammar with product token encoding device class:
 *   kaitu-router/{version} (...)  — k2r gateway (window._platform.platformType === 'gateway')
 *   kaitu-service/{version} (...) — all other clients (desktop, mobile, web)
 *
 * No other module may construct this header (single source of truth).
 */
function buildClientHeader(): string | null {
  const p = window._platform;
  if (!p?.version || !p?.os) return null;
  const cls = p.platformType === 'gateway' ? 'router' : 'service';
  const arch = p.arch || 'unknown';
  return `kaitu-${cls}/${p.version} (${p.os}; ${arch})`;
}

/** Auth paths where tokens should be auto-saved on success */
const AUTH_TOKEN_PATHS = [
  '/api/auth/login',
  '/api/auth/login/password',
  '/api/auth/register',
  '/api/auth/refresh',
];

/** Auth path where tokens + cache should be cleared on success */
const AUTH_LOGOUT_PATH = '/api/auth/logout';

/** Total request timeout in milliseconds — restores the original 15s AbortController semantics. */
const REQUEST_TIMEOUT_MS = 15000;

/** Module-level refresh promise for concurrent 401 dedup */
let _refreshPromise: Promise<boolean> | null = null;

/** Sentinel value returned by withTimeout when the deadline is exceeded. */
const TIMEOUT_SENTINEL = Symbol('timeout');

/**
 * Race a promise against a deadline. Resolves with TIMEOUT_SENTINEL if the
 * deadline fires first; otherwise resolves/rejects as the original promise does.
 * Clears the timer on settle to avoid leaked timers.
 */
function withTimeout<T>(p: Promise<T>, ms: number): Promise<T | typeof TIMEOUT_SENTINEL> {
  let timer: ReturnType<typeof setTimeout>;
  const deadline = new Promise<typeof TIMEOUT_SENTINEL>((resolve) => {
    timer = setTimeout(() => resolve(TIMEOUT_SENTINEL), ms);
  });
  return Promise.race([p, deadline]).finally(() => clearTimeout(timer!));
}

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
      // 1. Get token and capture epoch to detect stale 401s later
      const requestEpoch = authService.getTokenEpoch();
      const token = await authService.getToken();

      // 2. Build headers
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        // Brand is baked at build time; the backend authenticates it against
        // users.brand (403003 on mismatch). Constant per artifact.
        'X-K2-Brand': getBrandId(),
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }
      // Attach client info for device version tracking
      const clientHeader = buildClientHeader();
      if (clientHeader) {
        headers['X-K2-Client'] = clientHeader;
      }

      // 3. Serialize body
      const bodyString = body !== undefined ? JSON.stringify(body) : undefined;

      // 4. Resolve transport (camouflage-node relay → direct fallback) and perform the request.
      // Wrap in withTimeout to preserve the original 15s total-request cap (§5.3 #5 c).
      const resultOrTimeout = await withTimeout(
        resolveAndFetch({ method, path, headers, body: bodyString }),
        REQUEST_TIMEOUT_MS
      );
      if (resultOrTimeout === TIMEOUT_SENTINEL) {
        return { code: -1, message: 'Request timeout' };
      }
      const result = resultOrTimeout;
      if (result.transport === 'fail') {
        console.error('[CloudAPI] transport failed (relay + direct):', method, path);
        return { code: -1, message: 'Network error' };
      }
      const httpStatus = result.status;

      // 5. Parse the JSON response
      let jsonResponse: SResponse<T>;
      try {
        jsonResponse = await result.json() as SResponse<T>;
      } catch {
        console.error('[CloudAPI] bad json:', method, path);
        return { code: -1, message: 'Network error' };
      }
      console.info('[CloudAPI] response:', method, path, 'status:', httpStatus, 'code:', jsonResponse.code);
      if (jsonResponse.code !== 0) {
        console.warn('[CloudAPI] response error:', method, path, 'code:', jsonResponse.code, 'msg:', jsonResponse.message);
      }

      // 6. Handle 403002: server detected device class mismatch
      // (e.g. phone token reused on a router). Clear session and open login dialog.
      // Mirrors the 401-with-no-refresh path: clearTokens + isAuthenticated=false
      // keeps UI gating consistent if the user dismisses the dialog.
      if (jsonResponse.code === 403002) {
        console.warn('[CloudAPI] device class mismatch (403002) — clearing session');
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        useLoginDialogStore.getState().open({
          trigger: 'device-class-mismatch',
          message: i18n.t('auth:auth.deviceClassMismatch'),
        });
        return jsonResponse;
      }

      // 6b. Handle 403003: token's user belongs to the other brand (should
      // never happen with baked-brand clients — indicates restored/stale
      // storage). Mirror the 403002 recovery: clear session, prompt re-login.
      if (jsonResponse.code === 403003) {
        console.warn('[CloudAPI] brand mismatch (403003) — clearing session');
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        useLoginDialogStore.getState().open({
          trigger: 'brand-mismatch',
          message: i18n.t('auth:auth.brandMismatch'),
        });
        return jsonResponse;
      }

      // 7. Handle 401: try token refresh (pass epoch to detect stale requests)
      if (httpStatus === 401 || jsonResponse.code === 401) {
        return await this._handle401<T>(method, path, body, requestEpoch);
      }

      // 8. Auto-handle auth paths + seed entry pool on success
      if (jsonResponse.code === 0) {
        await this._handleAuthPath(path, jsonResponse);
        this._seedEntryPool(path, jsonResponse);
      }

      // 9. Return the response as SResponse
      return jsonResponse;
    } catch (error) {
      console.error('[CloudAPI] unexpected error:', method, path, (error as Error).message || error);
      return { code: -1, message: 'Network error' };
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

  /** Seed the antiblock entry pool with camouflage-node descriptors from a
   *  successful tunnels response. Every tunnels fetch (even one that arrived via
   *  relay) reinforces the pool, so a user who connects once always holds live
   *  relay entries. */
  _seedEntryPool<T>(path: string, response: SResponse<T>): void {
    if (!path.includes('/tunnels')) return;
    const data = response.data as { items?: unknown } | undefined;
    if (!data || !Array.isArray(data.items)) return;
    try {
      const entries = nodeEntriesFromTunnels(data.items as any);
      if (entries.length > 0) addNodes(entries);
    } catch (e) {
      console.warn('[CloudAPI] entry-pool seed failed', e);
    }
  },

  /**
   * Handle 401 by refreshing the token and retrying the original request.
   * If refresh fails, clear tokens and set isAuthenticated = false.
   */
  async _handle401<T>(
    method: string,
    path: string,
    body?: unknown,
    requestEpoch?: number
  ): Promise<SResponse<T>> {
    try {
      // Stale 401 guard: if tokens were refreshed (e.g., by login) since this
      // request was made, skip clearing and retry with the fresh token.
      if (requestEpoch !== undefined && authService.getTokenEpoch() !== requestEpoch) {
        console.info(`[CloudAPI] 401 from stale request (epoch ${requestEpoch}→${authService.getTokenEpoch()}), re-dispatching with current token`);
        return await this.request<T>(method, path, body);
      }

      // Get refresh token — if null, skip refresh entirely
      const refreshToken = await authService.getRefreshToken();
      if (!refreshToken) {
        console.warn('[CloudAPI] 401 but no refresh token available');
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        return { code: 401, message: 'No refresh token' } as SResponse<T>;
      }
      console.info('[CloudAPI] 401 received, attempting token refresh...');

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
      const refreshHeaders: Record<string, string> = {
        'Content-Type': 'application/json',
        'X-K2-Brand': getBrandId(),
      };
      const clientHeader = buildClientHeader();
      if (clientHeader) {
        refreshHeaders['X-K2-Client'] = clientHeader;
      }
      const resultOrTimeout = await withTimeout(
        resolveAndFetch({
          method: 'POST',
          path: '/api/auth/refresh',
          headers: refreshHeaders,
          body: JSON.stringify({ refreshToken }),
        }),
        REQUEST_TIMEOUT_MS
      );
      if (resultOrTimeout === TIMEOUT_SENTINEL || resultOrTimeout.transport === 'fail') {
        await authService.clearTokens();
        useAuthStore.setState({ isAuthenticated: false });
        return false;
      }
      const result = resultOrTimeout;
      const refreshJson = await result.json() as SResponse<{ token: string; refreshToken: string }>;
      if (refreshJson.code !== 0 || result.status >= 400) {
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
