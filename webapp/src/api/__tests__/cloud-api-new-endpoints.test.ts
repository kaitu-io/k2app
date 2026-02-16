import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { cloudApi, setAuthToken } from '../cloud';

// Mock the antiblock module
vi.mock('../antiblock', () => ({
  resolveEntry: vi.fn().mockResolvedValue('https://api.example.com'),
}));

describe('cloud API new endpoints', () => {
  let mockFetch: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ code: 0, message: 'ok', data: {} }),
    });
    vi.stubGlobal('fetch', mockFetch);
    setAuthToken('test-token');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ---------- test_cloud_api_endpoints ----------
  // Parameterized test for each new endpoint

  describe('test_cloud_api_endpoints', () => {
    const endpointCases: Array<{
      name: string;
      call: () => Promise<unknown>;
      expectedMethod: string;
      expectedPath: string;
      expectedBody?: unknown;
    }> = [
      {
        name: 'logout',
        call: () => (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).logout!(),
        expectedMethod: 'POST',
        expectedPath: '/api/auth/logout',
      },
      {
        name: 'getPlans',
        call: () => (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getPlans!(),
        expectedMethod: 'GET',
        expectedPath: '/api/plans',
      },
      {
        name: 'createOrder',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).createOrder!(
            'plan-123',
            'monthly',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/orders',
        expectedBody: { planId: 'plan-123', period: 'monthly' },
      },
      {
        name: 'previewOrder',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).previewOrder!(
            'plan-123',
            'monthly',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/orders/preview',
        expectedBody: { planId: 'plan-123', period: 'monthly' },
      },
      {
        name: 'getDevices',
        call: () => (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getDevices!(),
        expectedMethod: 'GET',
        expectedPath: '/api/devices',
      },
      {
        name: 'deleteDevice',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).deleteDevice!('device-1'),
        expectedMethod: 'DELETE',
        expectedPath: '/api/devices/device-1',
      },
      {
        name: 'updateDeviceRemark',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).updateDeviceRemark!(
            'device-1',
            'My Phone',
          ),
        expectedMethod: 'PUT',
        expectedPath: '/api/devices/device-1/remark',
        expectedBody: { remark: 'My Phone' },
      },
      {
        name: 'updateLanguage',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).updateLanguage!('zh-CN'),
        expectedMethod: 'PUT',
        expectedPath: '/api/user/language',
        expectedBody: { language: 'zh-CN' },
      },
      {
        name: 'getProHistories',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getProHistories!(),
        expectedMethod: 'GET',
        expectedPath: '/api/user/pro-histories',
      },
      {
        name: 'updateEmail',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).updateEmail!(
            'new@example.com',
            '123456',
          ),
        expectedMethod: 'PUT',
        expectedPath: '/api/user/email',
        expectedBody: { email: 'new@example.com', code: '123456' },
      },
      {
        name: 'sendEmailCode',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).sendEmailCode!(
            'user@example.com',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/user/email/code',
        expectedBody: { email: 'user@example.com' },
      },
      {
        name: 'setPassword',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).setPassword!('newpass123'),
        expectedMethod: 'PUT',
        expectedPath: '/api/user/password',
        expectedBody: { password: 'newpass123' },
      },
      {
        name: 'getMembers',
        call: () => (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getMembers!(),
        expectedMethod: 'GET',
        expectedPath: '/api/members',
      },
      {
        name: 'addMember',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).addMember!(
            'member@example.com',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/members',
        expectedBody: { email: 'member@example.com' },
      },
      {
        name: 'deleteMember',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).deleteMember!('member-1'),
        expectedMethod: 'DELETE',
        expectedPath: '/api/members/member-1',
      },
      {
        name: 'getLatestInviteCode',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getLatestInviteCode!(),
        expectedMethod: 'GET',
        expectedPath: '/api/invite-codes/latest',
      },
      {
        name: 'getInviteCodes',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getInviteCodes!(),
        expectedMethod: 'GET',
        expectedPath: '/api/invite-codes',
      },
      {
        name: 'createInviteCode',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).createInviteCode!(),
        expectedMethod: 'POST',
        expectedPath: '/api/invite-codes',
      },
      {
        name: 'updateInviteCodeRemark',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).updateInviteCodeRemark!(
            'code-1',
            'For friend',
          ),
        expectedMethod: 'PUT',
        expectedPath: '/api/invite-codes/code-1/remark',
        expectedBody: { remark: 'For friend' },
      },
      {
        name: 'createShareLink',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).createShareLink!(
            'invite-code-abc',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/share-links',
        expectedBody: { inviteCode: 'invite-code-abc' },
      },
      {
        name: 'getIssues',
        call: () => (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getIssues!(),
        expectedMethod: 'GET',
        expectedPath: '/api/issues',
      },
      {
        name: 'getIssueDetail',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getIssueDetail!(
            'issue-1',
          ),
        expectedMethod: 'GET',
        expectedPath: '/api/issues/issue-1',
      },
      {
        name: 'createIssue',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).createIssue!(
            'Bug report',
            'Something is broken',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/issues',
        expectedBody: { title: 'Bug report', content: 'Something is broken' },
      },
      {
        name: 'addComment',
        call: () =>
          (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).addComment!(
            'issue-1',
            'I can reproduce this',
          ),
        expectedMethod: 'POST',
        expectedPath: '/api/issues/issue-1/comments',
        expectedBody: { content: 'I can reproduce this' },
      },
      {
        name: 'getAppConfig',
        call: () => cloudApi.getAppConfig(),
        expectedMethod: 'GET',
        expectedPath: '/api/app/config',
      },
    ];

    it.each(endpointCases)(
      '$name calls $expectedMethod $expectedPath',
      async ({ call, expectedMethod, expectedPath, expectedBody }) => {
        await call();

        expect(mockFetch).toHaveBeenCalledWith(
          `https://api.example.com${expectedPath}`,
          expect.objectContaining({
            method: expectedMethod,
            ...(expectedBody !== undefined
              ? { body: JSON.stringify(expectedBody) }
              : {}),
          }),
        );
      },
    );
  });

  // ---------- test_auth_token_auto_included ----------

  describe('test_auth_token_auto_included', () => {
    it('includes Authorization header on authenticated requests', async () => {
      setAuthToken('secret-bearer-token');

      // Call a new endpoint that requires auth
      await (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getDevices!();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer secret-bearer-token',
          }),
        }),
      );
    });

    it('omits Authorization header when no token is set', async () => {
      setAuthToken(null);

      await (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getPlans!();

      const callHeaders = mockFetch.mock.calls[0]![1].headers as Record<string, string>;
      expect(callHeaders['Authorization']).toBeUndefined();
    });

    it('includes auth token for logout endpoint', async () => {
      setAuthToken('logout-token');

      await (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).logout!();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer logout-token',
          }),
        }),
      );
    });

    it('includes auth token for write operations', async () => {
      setAuthToken('write-token');

      await (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).createIssue!(
        'Title',
        'Body',
      );

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer write-token',
          }),
        }),
      );
    });
  });

  // ---------- test_token_refresh_on_401 ----------

  describe('test_token_refresh_on_401', () => {
    it('retries request after refreshing token on 401 response', async () => {
      // First call returns 401
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        json: () => Promise.resolve({ code: 401, message: 'token expired' }),
      });

      // Token refresh call succeeds
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { accessToken: 'new-token', refreshToken: 'new-refresh' },
          }),
      });

      // Retried original call succeeds
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { devices: [] },
          }),
      });

      const result = await (
        cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>
      ).getDevices!();

      // Should have made 3 fetch calls: original, refresh, retry
      expect(mockFetch).toHaveBeenCalledTimes(3);

      // First call was the original request
      expect(mockFetch.mock.calls[0]![0]).toBe('https://api.example.com/api/devices');

      // Second call was the token refresh
      expect(mockFetch.mock.calls[1]![0]).toBe('https://api.example.com/api/auth/refresh');

      // Third call retries the original with new token
      expect(mockFetch.mock.calls[2]![0]).toBe('https://api.example.com/api/devices');
      const retryHeaders = mockFetch.mock.calls[2]![1].headers as Record<string, string>;
      expect(retryHeaders['Authorization']).toBe('Bearer new-token');

      // Result should be from the successful retry
      expect(result).toEqual(
        expect.objectContaining({
          code: 0,
          data: { devices: [] },
        }),
      );
    });

    it('throws if token refresh also fails', async () => {
      // Original call returns 401
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        json: () => Promise.resolve({ code: 401, message: 'token expired' }),
      });

      // Token refresh also fails
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        json: () => Promise.resolve({ code: 401, message: 'refresh token expired' }),
      });

      await expect(
        (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getDevices!(),
      ).rejects.toThrow();
    });

    it('does not retry on non-401 errors', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        json: () => Promise.resolve({ code: 500, message: 'server error' }),
      });

      await expect(
        (cloudApi as Record<string, (...args: unknown[]) => Promise<unknown>>).getDevices!(),
      ).rejects.toThrow();

      // Should only have made 1 fetch call (no refresh attempt)
      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });
});
