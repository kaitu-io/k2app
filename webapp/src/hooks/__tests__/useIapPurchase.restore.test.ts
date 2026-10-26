import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));

const order: string[] = [];
const mockPost = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: (...args: any[]) => mockPost(...args) },
}));

const mockCacheGet = vi.fn();
vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    set: vi.fn(),
    get: (...args: any[]) => mockCacheGet(...args),
  },
}));

const mockRestoreDeviceAccount = vi.fn();
vi.mock('../../services/device-account', () => ({
  restoreDeviceAccount: (...args: any[]) => mockRestoreDeviceAccount(...args),
}));

vi.mock('../../utils/errorCode', async (orig) => ({
  ...(await orig<any>()),
  getErrorMessage: (code: number) => `msg:${code}`,
}));

import { useIapPurchase } from '../useIapPurchase';

const USER = { uuid: 'u1', expiredAt: 9999999999, loginIdentifies: [] };

function makeIap(txns: any[]) {
  return {
    getProducts: vi.fn().mockResolvedValue([]),
    purchase: vi.fn(),
    restore: vi.fn().mockResolvedValue(txns),
    finishTransaction: vi.fn().mockImplementation(async (id: string) => {
      order.push(`finish:${id}`);
    }),
    onTransactionUpdate: vi.fn().mockReturnValue(() => {}),
  };
}

describe('useIapPurchase restore 409004', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    order.length = 0;
    mockPost.mockReset();
    mockCacheGet.mockReset();
    mockRestoreDeviceAccount.mockReset();
  });
  afterEach(() => {
    (window as any)._platform = originalPlatform;
  });

  it('409004 zero-identity restores then re-verifies', async () => {
    mockCacheGet.mockReturnValue({ uuid: 'dev', loginIdentifies: [] });
    mockPost
      .mockImplementationOnce(async () => { order.push('verify'); return { code: 409004 }; })
      .mockImplementationOnce(async () => { order.push('verify'); return { code: 0, data: USER }; });
    mockRestoreDeviceAccount.mockImplementation(async () => { order.push('restore'); return true; });
    const iap = makeIap([{ transactionId: '1', jws: 'J' }]);
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.restore(); });

    expect(mockRestoreDeviceAccount).toHaveBeenCalledWith('J');
    expect(order).toEqual(['verify', 'restore', 'verify', 'finish:1']);
    expect(result.current.purchaseError).toBeNull();
  });

  it('409004 zero-identity but restore rejected: error, no finish, no re-verify', async () => {
    mockCacheGet.mockReturnValue({ uuid: 'dev', loginIdentifies: [] });
    mockPost.mockResolvedValue({ code: 409004 });
    mockRestoreDeviceAccount.mockResolvedValue(false);
    const iap = makeIap([{ transactionId: '1', jws: 'J' }]);
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.restore(); });

    expect(mockPost).toHaveBeenCalledTimes(1);
    expect(iap.finishTransaction).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('msg:409004');
  });

  it('409004 with identity shows owned-elsewhere message and does not finish', async () => {
    mockCacheGet.mockReturnValue({ uuid: 'a', loginIdentifies: [{ type: 'email', value: 'a@b.c' }] });
    mockPost.mockResolvedValue({ code: 409004 });
    const iap = makeIap([{ transactionId: '1', jws: 'J' }]);
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.restore(); });

    expect(mockRestoreDeviceAccount).not.toHaveBeenCalled();
    expect(iap.finishTransaction).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('msg:409004');
  });

  it('409004 without jws (old shell) shows owned-elsewhere message', async () => {
    mockCacheGet.mockReturnValue({ uuid: 'dev', loginIdentifies: [] });
    mockPost.mockResolvedValue({ code: 409004 });
    const iap = makeIap([{ transactionId: '1' }]);
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.restore(); });

    expect(mockRestoreDeviceAccount).not.toHaveBeenCalled();
    expect(iap.finishTransaction).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('msg:409004');
  });
});
