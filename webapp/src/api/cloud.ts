import { resolveEntry } from './antiblock';
import type { ApiResponse } from './types';

let authToken: string | null = null;
let storedRefreshToken: string | null = null;

export function setAuthToken(token: string | null): void {
  authToken = token;
}

export function setRefreshToken(token: string | null): void {
  storedRefreshToken = token;
}

export function getAuthToken(): string | null {
  return authToken;
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
    if (resp.status === 401 && authToken) {
      // Attempt token refresh
      const refreshEntry = await resolveEntry();
      const refreshUrl = `${refreshEntry}/api/auth/refresh`;
      const refreshHeaders: Record<string, string> = {
        'Content-Type': 'application/json',
      };
      if (authToken) {
        refreshHeaders['Authorization'] = `Bearer ${authToken}`;
      }

      const refreshResp = await fetch(refreshUrl, {
        method: 'POST',
        headers: refreshHeaders,
        body: JSON.stringify({ refreshToken: storedRefreshToken }),
      });

      if (!refreshResp.ok) {
        throw new Error(`Cloud API error: ${refreshResp.status} ${refreshResp.statusText}`);
      }

      const refreshData = (await refreshResp.json()) as ApiResponse<{ accessToken: string; refreshToken: string }>;
      const newToken = refreshData.data?.accessToken;
      const newRefresh = refreshData.data?.refreshToken;

      if (newToken) {
        setAuthToken(newToken);
      }
      if (newRefresh) {
        storedRefreshToken = newRefresh;
      }

      // Retry original request with new token
      const retryHeaders: Record<string, string> = {
        'Content-Type': 'application/json',
      };
      if (authToken) {
        retryHeaders['Authorization'] = `Bearer ${authToken}`;
      }

      const retryResp = await fetch(url, {
        method,
        headers: retryHeaders,
        body: body ? JSON.stringify(body) : undefined,
      });

      if (!retryResp.ok) {
        throw new Error(`Cloud API error: ${retryResp.status} ${retryResp.statusText}`);
      }

      return retryResp.json() as Promise<ApiResponse<T>>;
    }

    throw new Error(`Cloud API error: ${resp.status} ${resp.statusText}`);
  }

  return resp.json() as Promise<ApiResponse<T>>;
}

export const cloudApi = {
  // Auth
  getAuthCode: (email: string) => cloudRequest('POST', '/api/auth/code', { email }),
  login: (email: string, code: string, udid: string) =>
    cloudRequest('POST', '/api/auth/login', { email, code, udid }),
  refreshToken: (refreshToken: string) =>
    cloudRequest('POST', '/api/auth/refresh', { refreshToken }),
  logout: () => cloudRequest('POST', '/api/auth/logout'),

  // User
  getUserInfo: () => cloudRequest('GET', '/api/user/info'),
  updateLanguage: (language: string) =>
    cloudRequest('PUT', '/api/user/language', { language }),
  getProHistories: () => cloudRequest('GET', '/api/user/pro-histories'),
  updateEmail: (email: string, code: string) =>
    cloudRequest('PUT', '/api/user/email', { email, code }),
  sendEmailCode: (email: string) =>
    cloudRequest('POST', '/api/user/email/code', { email }),
  setPassword: (password: string) =>
    cloudRequest('PUT', '/api/user/password', { password }),

  // Plans & Orders
  getPlans: () => cloudRequest('GET', '/api/plans'),
  createOrder: (planId: string, period: string) =>
    cloudRequest('POST', '/api/orders', { planId, period }),
  previewOrder: (planId: string, period: string) =>
    cloudRequest('POST', '/api/orders/preview', { planId, period }),

  // Tunnels
  getTunnels: () => cloudRequest('GET', '/api/tunnels'),

  // Devices
  getDevices: () => cloudRequest('GET', '/api/devices'),
  deleteDevice: (deviceId: string) =>
    cloudRequest('DELETE', `/api/devices/${deviceId}`),
  updateDeviceRemark: (deviceId: string, remark: string) =>
    cloudRequest('PUT', `/api/devices/${deviceId}/remark`, { remark }),

  // Members
  getMembers: () => cloudRequest('GET', '/api/members'),
  addMember: (email: string) =>
    cloudRequest('POST', '/api/members', { email }),
  deleteMember: (memberId: string) =>
    cloudRequest('DELETE', `/api/members/${memberId}`),

  // Invite Codes
  getLatestInviteCode: () => cloudRequest('GET', '/api/invite-codes/latest'),
  getInviteCodes: () => cloudRequest('GET', '/api/invite-codes'),
  createInviteCode: () => cloudRequest('POST', '/api/invite-codes'),
  updateInviteCodeRemark: (codeId: string, remark: string) =>
    cloudRequest('PUT', `/api/invite-codes/${codeId}/remark`, { remark }),

  // Share Links
  createShareLink: (inviteCode: string) =>
    cloudRequest('POST', '/api/share-links', { inviteCode }),

  // Issues & Comments
  getIssues: () => cloudRequest('GET', '/api/issues'),
  getIssueDetail: (issueId: string) =>
    cloudRequest('GET', `/api/issues/${issueId}`),
  createIssue: (title: string, content: string) =>
    cloudRequest('POST', '/api/issues', { title, content }),
  addComment: (issueId: string, content: string) =>
    cloudRequest('POST', `/api/issues/${issueId}/comments`, { content }),

  // App Config
  getAppConfig: () => cloudRequest('GET', '/api/app/config'),
};
