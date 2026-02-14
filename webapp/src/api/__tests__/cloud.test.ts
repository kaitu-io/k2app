import { describe, it, expect, vi, beforeEach } from 'vitest';
import { cloudApi, setAuthToken } from '../cloud';

// Mock the antiblock module
vi.mock('../antiblock', () => ({
  resolveEntry: vi.fn().mockResolvedValue('https://api.example.com'),
}));

describe('cloudApi', () => {
  let mockFetch: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ code: 0, message: 'ok', data: {} }),
    });
    vi.stubGlobal('fetch', mockFetch);
    setAuthToken(null);
  });

  describe('login', () => {
    it('calls ${entry}/api/auth/login with correct body', async () => {
      await cloudApi.login('test@example.com', '123456', 'device-id');

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/api/auth/login',
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
          body: JSON.stringify({ email: 'test@example.com', code: '123456', udid: 'device-id' }),
        }),
      );
    });
  });

  describe('getAuthCode', () => {
    it('sends POST to /api/auth/code with email', async () => {
      await cloudApi.getAuthCode('user@test.com');

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/api/auth/code',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ email: 'user@test.com' }),
        }),
      );
    });
  });

  describe('getUserInfo', () => {
    it('sends GET to /api/user/info', async () => {
      await cloudApi.getUserInfo();

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/api/user/info',
        expect.objectContaining({
          method: 'GET',
          body: undefined,
        }),
      );
    });
  });

  describe('auth token', () => {
    it('includes Authorization header when token is set', async () => {
      setAuthToken('my-token');
      await cloudApi.getUserInfo();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer my-token',
          }),
        }),
      );
    });

    it('does not include Authorization header when token is null', async () => {
      setAuthToken(null);
      await cloudApi.getUserInfo();

      const headers = mockFetch.mock.calls[0]![1].headers as Record<string, string>;
      expect(headers['Authorization']).toBeUndefined();
    });
  });

  describe('error handling', () => {
    it('throws on non-ok response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
      });

      await expect(cloudApi.getUserInfo()).rejects.toThrow('Cloud API error: 401 Unauthorized');
    });
  });

  describe('getTunnels', () => {
    it('sends GET to /api/tunnels', async () => {
      await cloudApi.getTunnels();

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/api/tunnels',
        expect.objectContaining({ method: 'GET' }),
      );
    });
  });

  describe('refreshToken', () => {
    it('sends POST to /api/auth/refresh with refreshToken', async () => {
      await cloudApi.refreshToken('refresh-token-123');

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/api/auth/refresh',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ refreshToken: 'refresh-token-123' }),
        }),
      );
    });
  });
});
