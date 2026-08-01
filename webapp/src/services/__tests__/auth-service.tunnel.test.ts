import { describe, it, expect, vi, beforeEach } from 'vitest';
import { authService, TOKEN_STORAGE_KEY, TUNNEL_TOKEN_STORAGE_KEY } from '../auth-service';

// 内存版 _platform.storage stub。
function makeStorage(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial));
  return {
    get: vi.fn(async (k: string) => store.get(k) ?? null),
    set: vi.fn(async (k: string, v: string) => void store.set(k, v)),
    remove: vi.fn(async (k: string) => void store.delete(k)),
    has: vi.fn(async (k: string) => store.has(k)),
    keys: vi.fn(async () => [...store.keys()]),
    clear: vi.fn(async () => void store.clear()),
  };
}

vi.mock('../device-udid', () => ({
  getDeviceUdid: vi.fn(),
}));
import { getDeviceUdid } from '../device-udid';

describe('authService tunnel token', () => {
  beforeEach(() => {
    // vi.clearAllMocks() 会清掉实现 —— 每个 beforeEach 里重设。
    vi.clearAllMocks();
    vi.mocked(getDeviceUdid).mockResolvedValue('UDID-1');
  });

  it('getCredentials prefers tunnel token over access token', async () => {
    (window as any)._platform = {
      storage: makeStorage({
        [TOKEN_STORAGE_KEY]: 'ACCESS',
        [TUNNEL_TOKEN_STORAGE_KEY]: 'TUNNEL',
      }),
    };
    const { udid, token } = await authService.getCredentials();
    expect(udid).toBe('UDID-1');
    expect(token).toBe('TUNNEL');
  });

  it('getCredentials falls back to access token when no tunnel token yet', async () => {
    (window as any)._platform = {
      storage: makeStorage({ [TOKEN_STORAGE_KEY]: 'ACCESS' }),
    };
    const { token } = await authService.getCredentials();
    expect(token).toBe('ACCESS');
  });

  it('clearTokens removes the tunnel token too', async () => {
    const storage = makeStorage({
      [TOKEN_STORAGE_KEY]: 'ACCESS',
      [TUNNEL_TOKEN_STORAGE_KEY]: 'TUNNEL',
    });
    (window as any)._platform = { storage };
    await authService.clearTokens();
    expect(storage.remove).toHaveBeenCalledWith(TUNNEL_TOKEN_STORAGE_KEY);
  });

  it('setTunnelToken persists under k2.auth.tunnel_token', async () => {
    const storage = makeStorage();
    (window as any)._platform = { storage };
    await authService.setTunnelToken('T90');
    expect(storage.set).toHaveBeenCalledWith('k2.auth.tunnel_token', 'T90');
  });
});
