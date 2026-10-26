import { describe, it, expect, vi, beforeEach } from 'vitest';

const mockPost = vi.fn();
const mockGet = vi.fn();
vi.mock('../cloud-api', () => ({
  cloudApi: {
    post: (...args: unknown[]) => mockPost(...args),
    get: (...args: unknown[]) => mockGet(...args),
  },
}));

const mockHasToken = vi.fn();
vi.mock('../auth-service', () => ({
  authService: { hasToken: () => mockHasToken() },
}));

const mockGetDeviceUdid = vi.fn();
const mockRegenerateDeviceUdid = vi.fn();
vi.mock('../device-udid', () => ({
  getDeviceUdid: () => mockGetDeviceUdid(),
  regenerateDeviceUdid: () => mockRegenerateDeviceUdid(),
}));

const mockCacheGet = vi.fn();
const mockCacheSet = vi.fn();
const mockCacheClear = vi.fn();
vi.mock('../cache-store', () => ({
  cacheStore: {
    get: (...args: unknown[]) => mockCacheGet(...args),
    set: (...args: unknown[]) => mockCacheSet(...args),
    clear: () => mockCacheClear(),
  },
}));

const mockTrackFunnelOnce = vi.fn();
vi.mock('../stats', () => ({
  statsService: { trackFunnelOnce: (...args: unknown[]) => mockTrackFunnelOnce(...args) },
}));

const mockSetIsAuthenticated = vi.fn();
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: { getState: () => ({ setIsAuthenticated: mockSetIsAuthenticated }) },
}));

vi.mock('../../i18n/i18n', () => ({
  default: { language: 'zh-CN', t: (k: string) => k },
}));

const mockIapRestore = vi.fn();

function setPlatform(extra: Record<string, unknown> = {}) {
  (window as any)._platform = {
    keychainIdentity: true,
    iap: { restore: () => mockIapRestore() },
    ...extra,
  };
}

async function freshImport() {
  vi.resetModules();
  return import('../device-account');
}

beforeEach(() => {
  vi.clearAllMocks();
  setPlatform();
  mockHasToken.mockResolvedValue(false);
  mockGetDeviceUdid.mockResolvedValue('udid-1');
  mockRegenerateDeviceUdid.mockResolvedValue('udid-2');
  mockCacheGet.mockReturnValue({ deviceAccount: true });
  mockGet.mockResolvedValue({ code: 0, data: { deviceAccount: true } });
  mockPost.mockResolvedValue({ code: 0, data: { accessToken: 'a', refreshToken: 'r' } });
  mockIapRestore.mockResolvedValue([]);
  mockTrackFunnelOnce.mockResolvedValue(undefined);
});

describe('ensureDeviceAccount', () => {
  it('old shell never calls device-account', async () => {
    setPlatform({ keychainIdentity: undefined });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('skipped');
    expect(mockPost).toHaveBeenCalledTimes(0);
  });

  it('keychainIdentity false (Android / old iOS) skips', async () => {
    setPlatform({ keychainIdentity: false });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('skipped');
    expect(mockPost).toHaveBeenCalledTimes(0);
  });

  it('reinstall with keychain token skips device-account', async () => {
    mockHasToken.mockResolvedValue(true);
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('existing');
    expect(mockPost).toHaveBeenCalledTimes(0);
  });

  it('server switch off skips', async () => {
    mockCacheGet.mockReturnValue({ deviceAccount: false });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('skipped');
    expect(mockPost).toHaveBeenCalledTimes(0);
  });

  it('fetches and caches app config when not cached', async () => {
    mockCacheGet.mockReturnValue(null);
    mockGet.mockResolvedValue({ code: 0, data: { deviceAccount: false } });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('skipped');
    expect(mockGet).toHaveBeenCalledWith('/api/app/config');
    expect(mockCacheSet).toHaveBeenCalledWith('api:app_config', { deviceAccount: false }, { ttl: 3600 });
    expect(mockPost).toHaveBeenCalledTimes(0);
  });

  it('creates account and marks authenticated', async () => {
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('created');
    expect(mockPost).toHaveBeenCalledTimes(1);
    expect(mockPost).toHaveBeenCalledWith('/api/auth/device-account', {
      udid: 'udid-1',
      remark: 'startup:startup.newDevice',
      language: 'zh-CN',
    });
    expect(mockTrackFunnelOnce).toHaveBeenCalledWith('device_account_created');
    expect(mockCacheClear).toHaveBeenCalled();
    expect(mockSetIsAuthenticated).toHaveBeenCalledWith(true);
  });

  it('concurrent calls share one request', async () => {
    const { ensureDeviceAccount } = await freshImport();
    const [a, b] = await Promise.all([ensureDeviceAccount(), ensureDeviceAccount()]);
    expect(a).toBe('created');
    expect(b).toBe('created');
    expect(mockPost).toHaveBeenCalledTimes(1);
  });

  it('403006 with entitlement restores', async () => {
    mockPost
      .mockResolvedValueOnce({ code: 403006, message: 'proof required' })
      .mockResolvedValueOnce({ code: 0, data: { accessToken: 'a', refreshToken: 'r' } });
    mockIapRestore.mockResolvedValue([
      { transactionId: '0', productId: 'p' },
      { transactionId: '1', productId: 'p', jws: 'J' },
    ]);
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('restored');
    expect(mockPost).toHaveBeenCalledTimes(2);
    const [path, body] = mockPost.mock.calls[1];
    expect(path).toBe('/api/auth/device-account/restore');
    expect(body.signedTransaction).toBe('J');
    expect(body.udid).toBe('udid-1');
    expect(mockRegenerateDeviceUdid).not.toHaveBeenCalled();
    expect(mockSetIsAuthenticated).toHaveBeenCalledWith(true);
  });

  it('403006 with entitlement but failed restore returns login-required, keeps udid', async () => {
    mockPost
      .mockResolvedValueOnce({ code: 403006 })
      .mockResolvedValueOnce({ code: 409004 });
    mockIapRestore.mockResolvedValue([{ transactionId: '1', productId: 'p', jws: 'J' }]);
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
    expect(mockRegenerateDeviceUdid).not.toHaveBeenCalled();
    expect(mockSetIsAuthenticated).not.toHaveBeenCalled();
  });

  it('403006 without entitlement regenerates udid and recreates', async () => {
    mockPost
      .mockResolvedValueOnce({ code: 403006 })
      .mockResolvedValueOnce({ code: 0, data: { accessToken: 'a', refreshToken: 'r' } });
    mockIapRestore.mockResolvedValue([]);
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('created');
    expect(mockRegenerateDeviceUdid).toHaveBeenCalledTimes(1);
    expect(mockPost).toHaveBeenCalledTimes(2);
    expect(mockPost.mock.calls[0][0]).toBe('/api/auth/device-account');
    expect(mockPost.mock.calls[1][0]).toBe('/api/auth/device-account');
    expect(mockPost.mock.calls[1][1].udid).toBe('udid-2');
  });

  it('403006 retry only once', async () => {
    mockPost.mockResolvedValue({ code: 403006 });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
    expect(mockRegenerateDeviceUdid).toHaveBeenCalledTimes(1);
    expect(mockPost).toHaveBeenCalledTimes(2);
  });

  it('403006 with transactions but no jws keeps udid', async () => {
    mockPost.mockResolvedValueOnce({ code: 403006 });
    mockIapRestore.mockResolvedValue([{ transactionId: '1', productId: 'p' }]);
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
    expect(mockRegenerateDeviceUdid).not.toHaveBeenCalled();
    expect(mockPost).toHaveBeenCalledTimes(1);
  });

  it('403006 when StoreKit query fails does not regenerate', async () => {
    mockPost.mockResolvedValueOnce({ code: 403006 });
    mockIapRestore.mockRejectedValue(new Error('storekit'));
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
    expect(mockRegenerateDeviceUdid).not.toHaveBeenCalled();
  });

  it('403005 returns login-required', async () => {
    mockPost.mockResolvedValue({ code: 403005 });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
    expect(mockPost).toHaveBeenCalledTimes(1);
    expect(mockSetIsAuthenticated).not.toHaveBeenCalled();
  });

  it('403004 returns login-required', async () => {
    mockPost.mockResolvedValue({ code: 403004 });
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
  });

  it('network failure returns login-required', async () => {
    mockPost.mockRejectedValue(new Error('offline'));
    const { ensureDeviceAccount } = await freshImport();
    expect(await ensureDeviceAccount()).toBe('login-required');
  });
});

describe('restoreDeviceAccount', () => {
  it('posts udid + signedTransaction + remark and marks authenticated', async () => {
    const { restoreDeviceAccount } = await freshImport();
    expect(await restoreDeviceAccount('JWS')).toBe(true);
    expect(mockPost).toHaveBeenCalledWith('/api/auth/device-account/restore', {
      udid: 'udid-1',
      signedTransaction: 'JWS',
      remark: 'startup:startup.newDevice',
    });
    expect(mockCacheClear).toHaveBeenCalled();
    expect(mockSetIsAuthenticated).toHaveBeenCalledWith(true);
  });

  it('returns false on server rejection', async () => {
    mockPost.mockResolvedValue({ code: 409004 });
    const { restoreDeviceAccount } = await freshImport();
    expect(await restoreDeviceAccount('JWS')).toBe(false);
    expect(mockSetIsAuthenticated).not.toHaveBeenCalled();
  });
});
