/**
 * API Client Tests
 *
 * Tests for the web dashboard API client
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  api,
  ApiError,
  UnauthorizedError,
  ErrorCode,
  getContactUrl,
  getContactTypeName,
} from '../api';
import type { ContactInfo } from '../api';

// Mock fetch globally
const mockFetch = vi.fn();
global.fetch = mockFetch;

// Mock toast
vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

// Mock appEvents
vi.mock('../events', () => ({
  appEvents: {
    emit: vi.fn(),
    on: vi.fn(),
    off: vi.fn(),
  },
}));

describe('API Client', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset localStorage
    localStorage.clear();
    // Reset document.cookie
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: '',
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('request', () => {
    it('should make successful GET request', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, data: { id: 1, name: 'test' } }),
        headers: new Headers({ 'Content-Length': '50' }),
        status: 200,
      });

      const result = await api.request<{ id: number; name: string }>('/api/test');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/test'),
        expect.objectContaining({
          credentials: 'include',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
          }),
        })
      );
      expect(result).toEqual({ id: 1, name: 'test' });
    });

    it('should make POST request with body', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, data: { success: true } }),
        headers: new Headers({ 'Content-Length': '30' }),
        status: 200,
      });

      const result = await api.request('/api/create', {
        method: 'POST',
        body: JSON.stringify({ name: 'test' }),
      });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ name: 'test' }),
        })
      );
      expect(result).toEqual({ success: true });
    });

    it('should handle network errors', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      await expect(api.request('/api/test')).rejects.toThrow('网络连接失败');
    });

    it('should handle server errors (non-ok response)', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        headers: new Headers(),
      });

      await expect(api.request('/api/test')).rejects.toThrow('服务器错误: 500');
    });

    it('should handle API error codes', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 500, message: 'System error' }),
        headers: new Headers({ 'Content-Length': '40' }),
        status: 200,
      });

      await expect(api.request('/api/test')).rejects.toThrow(ApiError);
    });

    it('should handle empty response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        headers: new Headers({ 'Content-Length': '0' }),
        status: 200,
      });

      const result = await api.request('/api/test');
      expect(result).toEqual({});
    });

    it('should handle 204 No Content response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        headers: new Headers(),
        status: 204,
      });

      const result = await api.request('/api/test');
      expect(result).toEqual({});
    });
  });

  describe('authentication', () => {
    it('should include CSRF token in non-GET requests', async () => {
      // Set CSRF cookie
      Object.defineProperty(document, 'cookie', {
        writable: true,
        value: 'csrf_token=test-csrf-token',
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, data: {} }),
        headers: new Headers({ 'Content-Length': '20' }),
        status: 200,
      });

      await api.request('/api/test', { method: 'POST' });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'X-CSRF-Token': 'test-csrf-token',
          }),
        })
      );
    });

    it('should include embed token when available', async () => {
      localStorage.setItem('embed_auth_token', 'embed-token-123');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, data: {} }),
        headers: new Headers({ 'Content-Length': '20' }),
        status: 200,
      });

      await api.request('/api/test');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer embed-token-123',
          }),
        })
      );
    });

    it('should handle 401 unauthorized error', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 401, message: 'Unauthorized' }),
        headers: new Headers({ 'Content-Length': '40' }),
        status: 200,
      });

      await expect(
        api.request('/api/protected', { autoRedirectToAuth: false })
      ).rejects.toThrow(ApiError);
    });
  });

  describe('getCSRFToken', () => {
    it('should extract CSRF token from cookie', () => {
      Object.defineProperty(document, 'cookie', {
        writable: true,
        value: 'other=value; csrf_token=my-csrf-token; another=cookie',
      });

      const token = api.getCSRFToken();
      expect(token).toBe('my-csrf-token');
    });

    it('should return null if no CSRF token', () => {
      Object.defineProperty(document, 'cookie', {
        writable: true,
        value: 'other=value',
      });

      const token = api.getCSRFToken();
      expect(token).toBeNull();
    });
  });

  describe('clearAuthData', () => {
    it('should clear embed token from localStorage', () => {
      localStorage.setItem('embed_auth_token', 'test-token');

      api.clearAuthData();

      expect(localStorage.getItem('embed_auth_token')).toBeNull();
    });
  });
});

describe('ApiError', () => {
  it('should create error with code and message', () => {
    const error = new ApiError(ErrorCode.NotFound, 'Resource not found');

    expect(error.code).toBe(404);
    expect(error.message).toBe('Resource not found');
    expect(error.name).toBe('ApiError');
  });

  it('should check if unauthorized', () => {
    const authError = new ApiError(ErrorCode.NotLogin, 'Not logged in');
    const otherError = new ApiError(ErrorCode.NotFound, 'Not found');

    expect(authError.isUnauthorized()).toBe(true);
    expect(otherError.isUnauthorized()).toBe(false);
  });

  it('should check if forbidden', () => {
    const forbiddenError = new ApiError(ErrorCode.Forbidden, 'Forbidden');
    const otherError = new ApiError(ErrorCode.NotLogin, 'Not logged in');

    expect(forbiddenError.isForbidden()).toBe(true);
    expect(otherError.isForbidden()).toBe(false);
  });

  it('should check if not found', () => {
    const notFoundError = new ApiError(ErrorCode.NotFound, 'Not found');
    const otherError = new ApiError(ErrorCode.NotLogin, 'Not logged in');

    expect(notFoundError.isNotFound()).toBe(true);
    expect(otherError.isNotFound()).toBe(false);
  });
});

describe('UnauthorizedError', () => {
  it('should be an instance of ApiError', () => {
    const error = new UnauthorizedError();

    expect(error).toBeInstanceOf(ApiError);
    expect(error.code).toBe(ErrorCode.NotLogin);
  });

  it('should have default message', () => {
    const error = new UnauthorizedError();

    expect(error.message).toBe('Unauthorized');
  });

  it('should accept custom message', () => {
    const error = new UnauthorizedError('Session expired');

    expect(error.message).toBe('Session expired');
  });
});

describe('ErrorCode constants', () => {
  it('should have correct values', () => {
    expect(ErrorCode.None).toBe(0);
    expect(ErrorCode.InvalidOperation).toBe(400);
    expect(ErrorCode.NotLogin).toBe(401);
    expect(ErrorCode.PaymentRequired).toBe(402);
    expect(ErrorCode.Forbidden).toBe(403);
    expect(ErrorCode.NotFound).toBe(404);
    expect(ErrorCode.Conflict).toBe(409);
    expect(ErrorCode.InvalidArgument).toBe(422);
    expect(ErrorCode.TooManyRequests).toBe(429);
    expect(ErrorCode.SystemError).toBe(500);
    expect(ErrorCode.ServiceUnavailable).toBe(503);
  });

  it('should have custom error codes', () => {
    expect(ErrorCode.InvalidCampaignCode).toBe(400001);
    expect(ErrorCode.InvalidClientClock).toBe(400002);
  });
});

describe('Contact Utilities', () => {
  describe('getContactUrl', () => {
    it('should generate Telegram URL', () => {
      const contact: ContactInfo = { type: 'telegram', value: '@username' };
      expect(getContactUrl(contact)).toBe('https://t.me/username');
    });

    it('should handle Telegram URL without @', () => {
      const contact: ContactInfo = { type: 'telegram', value: 'username' };
      expect(getContactUrl(contact)).toBe('https://t.me/username');
    });

    it('should pass through Telegram link', () => {
      const contact: ContactInfo = { type: 'telegram', value: 'https://t.me/custom' };
      expect(getContactUrl(contact)).toBe('https://t.me/custom');
    });

    it('should generate email mailto URL', () => {
      const contact: ContactInfo = { type: 'email', value: 'test@example.com' };
      expect(getContactUrl(contact)).toBe('mailto:test@example.com');
    });

    it('should generate Signal URL', () => {
      const contact: ContactInfo = { type: 'signal', value: '+1234567890' };
      expect(getContactUrl(contact)).toBe('https://signal.me/#p/+1234567890');
    });

    it('should generate WhatsApp URL', () => {
      const contact: ContactInfo = { type: 'whatsapp', value: '+1234567890' };
      expect(getContactUrl(contact)).toBe('https://wa.me/+1234567890');
    });

    it('should generate Line URL', () => {
      const contact: ContactInfo = { type: 'line', value: 'lineid' };
      expect(getContactUrl(contact)).toBe('https://line.me/ti/p/~lineid');
    });

    it('should return null for WeChat without URL', () => {
      const contact: ContactInfo = { type: 'wechat', value: 'wechat_id' };
      expect(getContactUrl(contact)).toBeNull();
    });

    it('should return URL for WeChat with link', () => {
      const contact: ContactInfo = { type: 'wechat', value: 'https://qr.wechat.com/xxx' };
      expect(getContactUrl(contact)).toBe('https://qr.wechat.com/xxx');
    });

    it('should handle other type with URL', () => {
      const contact: ContactInfo = { type: 'other', value: 'https://custom.link' };
      expect(getContactUrl(contact)).toBe('https://custom.link');
    });

    it('should return null for other type without URL', () => {
      const contact: ContactInfo = { type: 'other', value: 'some text' };
      expect(getContactUrl(contact)).toBeNull();
    });
  });

  describe('getContactTypeName', () => {
    it('should return correct names for all types', () => {
      expect(getContactTypeName('telegram')).toBe('Telegram');
      expect(getContactTypeName('email')).toBe('Email');
      expect(getContactTypeName('signal')).toBe('Signal');
      expect(getContactTypeName('whatsapp')).toBe('WhatsApp');
      expect(getContactTypeName('wechat')).toBe('微信');
      expect(getContactTypeName('line')).toBe('Line');
      expect(getContactTypeName('other')).toBe('其他');
    });
  });
});

describe('API Methods', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('getPlans', () => {
    it('should fetch plans', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            data: {
              items: [{ pid: '1', label: 'Basic', price: 999 }],
            },
          }),
        headers: new Headers({ 'Content-Length': '100' }),
        status: 200,
      });

      const result = await api.getPlans();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/plans'),
        expect.any(Object)
      );
      expect(result.items).toHaveLength(1);
    });
  });

  describe('getUserProfile', () => {
    it('should fetch user profile', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            data: { uuid: 'user-1', expiredAt: Date.now() + 86400000 },
          }),
        headers: new Headers({ 'Content-Length': '100' }),
        status: 200,
      });

      const result = await api.getUserProfile();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/user/info'),
        expect.any(Object)
      );
      expect(result.uuid).toBe('user-1');
    });
  });

  describe('logout', () => {
    it('should call logout endpoint and clear auth data', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0 }),
        headers: new Headers({ 'Content-Length': '0' }),
        status: 200,
      });

      localStorage.setItem('embed_auth_token', 'test-token');

      await api.logout();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/auth/logout'),
        expect.objectContaining({ method: 'POST' })
      );
      expect(localStorage.getItem('embed_auth_token')).toBeNull();
    });

    it('should clear auth data even if logout fails', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      localStorage.setItem('embed_auth_token', 'test-token');

      await api.logout();

      expect(localStorage.getItem('embed_auth_token')).toBeNull();
    });
  });

  describe('updateBatchScript', () => {
    it('should send PUT request to update script', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            data: {
              id: 1,
              name: 'Updated Script',
              description: 'Updated description',
              content: 'echo "updated"',
              executeWithSudo: true,
              createdAt: 1700000000000,
              updatedAt: 1700000001000,
            },
          }),
        headers: new Headers({ 'Content-Length': '200' }),
        status: 200,
      });

      const result = await api.updateBatchScript(1, {
        name: 'Updated Script',
        description: 'Updated description',
        content: 'echo "updated"',
        executeWithSudo: true,
      });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/app/batch-scripts/1'),
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({
            name: 'Updated Script',
            description: 'Updated description',
            content: 'echo "updated"',
            executeWithSudo: true,
          }),
        })
      );
      expect(result.id).toBe(1);
      expect(result.name).toBe('Updated Script');
      expect(result.executeWithSudo).toBe(true);
    });
  });
});
