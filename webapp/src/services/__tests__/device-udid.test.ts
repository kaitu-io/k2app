import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock _platform.storage
const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  getAll: vi.fn(),
  clear: vi.fn(),
};

// Must mock crypto.randomUUID since jsdom doesn't have it
const MOCK_UUID = 'a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d';

beforeEach(() => {
  vi.clearAllMocks();
  (window as any)._platform = { storage: mockStorage };
  vi.stubGlobal('crypto', {
    randomUUID: () => MOCK_UUID,
    subtle: {
      digest: async (_algo: string, data: ArrayBuffer) => {
        // Simple deterministic mock: return data padded to 32 bytes
        const input = new Uint8Array(data);
        const result = new Uint8Array(32);
        for (let i = 0; i < 32; i++) {
          result[i] = input[i % input.length] ^ 0x42;
        }
        return result.buffer;
      },
    },
  });
});

// Must re-import to reset module-level cache between tests
async function freshImport() {
  vi.resetModules();
  return import('../device-udid');
}

describe('getDeviceUdid', () => {
  it('generates new UDID when storage is empty', async () => {
    mockStorage.get.mockResolvedValue(null);
    mockStorage.set.mockResolvedValue(undefined);
    mockStorage.remove.mockResolvedValue(undefined);

    const { getDeviceUdid } = await freshImport();
    const udid = await getDeviceUdid();

    // Should have stored the raw UUID
    expect(mockStorage.set).toHaveBeenCalledWith('device-udid', MOCK_UUID);
    // Should return 32 hex chars
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
    // Should have cleared stale auth tokens (migration guard)
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.auth.token');
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.auth.refresh');
  });

  it('returns existing UDID from storage without generating', async () => {
    const EXISTING_UUID = 'existing-uuid-from-storage';
    mockStorage.get.mockResolvedValue(EXISTING_UUID);

    const { getDeviceUdid } = await freshImport();
    const udid = await getDeviceUdid();

    // Should NOT generate or store a new UUID
    expect(mockStorage.set).not.toHaveBeenCalled();
    // Should NOT clear tokens
    expect(mockStorage.remove).not.toHaveBeenCalled();
    // Should return 32 hex chars
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
  });

  it('caches result on subsequent calls', async () => {
    mockStorage.get.mockResolvedValue('cached-test');

    const { getDeviceUdid } = await freshImport();
    const first = await getDeviceUdid();
    const second = await getDeviceUdid();

    expect(first).toBe(second);
    // Storage should only be read once
    expect(mockStorage.get).toHaveBeenCalledTimes(1);
  });

  it('throws if _platform.storage is not available', async () => {
    (window as any)._platform = undefined;

    const { getDeviceUdid } = await freshImport();
    await expect(getDeviceUdid()).rejects.toThrow('Platform storage not available');
  });

  it('migration guard is non-fatal if token removal fails', async () => {
    mockStorage.get.mockResolvedValue(null);
    mockStorage.set.mockResolvedValue(undefined);
    mockStorage.remove.mockRejectedValue(new Error('storage error'));

    const { getDeviceUdid } = await freshImport();
    // Should not throw despite remove() failing
    const udid = await getDeviceUdid();
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
  });
});
