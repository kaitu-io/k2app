/**
 * Cloud API Client - Stub (F2 RED phase)
 *
 * This module will replace window._k2.api with direct HTTP fetch() calls.
 * Currently a stub -- implementation comes in GREEN phase.
 *
 * Responsibilities:
 * - Base URL resolution (relative URLs for now, antiblock later)
 * - Auth header injection: Bearer token from authService.getToken()
 * - 401 handling: refresh token, retry once, then logout
 * - Standard fetch() wrapper returning SResponse format
 */

import type { SResponse } from '../types/kaitu-core';

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
    _method: string,
    _path: string,
    _body?: unknown
  ): Promise<SResponse<T>> {
    // TODO: Implement in GREEN phase
    throw new Error('[CloudApi] Not implemented yet');
  },
};
