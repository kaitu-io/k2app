import { resolveEntry } from './antiblock';
import type { ApiResponse } from './types';

let authToken: string | null = null;

export function setAuthToken(token: string | null): void {
  authToken = token;
}

async function cloudRequest<T>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>> {
  const entry = await resolveEntry();
  const url = `${entry}${path}`;

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const resp = await fetch(url, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!resp.ok) {
    throw new Error(`Cloud API error: ${resp.status} ${resp.statusText}`);
  }

  return resp.json() as Promise<ApiResponse<T>>;
}

export const cloudApi = {
  getAuthCode: (email: string) => cloudRequest('POST', '/api/auth/code', { email }),
  login: (email: string, code: string, udid: string) =>
    cloudRequest('POST', '/api/auth/login', { email, code, udid }),
  refreshToken: (refreshToken: string) =>
    cloudRequest('POST', '/api/auth/refresh', { refreshToken }),
  getUserInfo: () => cloudRequest('GET', '/api/user/info'),
  getTunnels: () => cloudRequest('GET', '/api/tunnels'),
  getAppConfig: () => cloudRequest('GET', '/api/app/config'),
};
