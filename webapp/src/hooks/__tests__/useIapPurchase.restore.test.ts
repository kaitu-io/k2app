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

  // 4.4.16 was rejected (App Review 5.1.1(v)) for telling a signed-in user to sign in to
  // the owner's account. The subscription follows the Apple ID: an identity account restores too.
  it('409004 while signed in with an identity also restores then re-verifies', async () => {
    mockCacheGet.mockReturnValue({ uuid: 'a', loginIdentifies: [{ type: 'email', value: 'a@b.c' }] });
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

  it('purchase hitting 409004 reads the JWS from StoreKit entitlements, restores, re-verifies', async () => {
    mockPost
      .mockImplementationOnce(async () => { order.push('verify'); return { code: 409004 }; })
      .mockImplementationOnce(async () => { order.push('verify'); return { code: 0, data: USER }; });
    mockRestoreDeviceAccount.mockImplementation(async () => { order.push('restore'); return true; });
    const iap = makeIap([{ transactionId: '9', jws: 'OTHER' }, { transactionId: '7', jws: 'MINE' }]);
    iap.purchase.mockResolvedValue({ result: 'success', transactionId: '7' });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.purchase('p', 'tok'); });

    expect(mockRestoreDeviceAccount).toHaveBeenCalledWith('MINE');
    expect(order).toEqual(['verify', 'restore', 'verify', 'finish:7']);
    expect(result.current.purchaseError).toBeNull();
  });

  it('a second 409004 after a successful restore does not loop', async () => {
    mockPost.mockResolvedValue({ code: 409004 });
    mockRestoreDeviceAccount.mockResolvedValue(true);
    const iap = makeIap([{ transactionId: '1', jws: 'J' }]);
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => { await result.current.restore(); });

    expect(mockRestoreDeviceAccount).toHaveBeenCalledTimes(1);
    expect(mockPost).toHaveBeenCalledTimes(2);
    expect(iap.finishTransaction).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('msg:409004');
  });

  it('a background transaction update hitting 409004 never switches accounts', async () => {
    mockPost.mockResolvedValue({ code: 409004 });
    const iap = makeIap([{ transactionId: '1', jws: 'J' }]);
    let update: ((d: any) => void) | undefined;
    iap.onTransactionUpdate.mockImplementation((cb: any) => { update = cb; return () => {}; });
    (window as any)._platform = { iap };

    renderHook(() => useIapPurchase());
    await act(async () => { update?.({ transactionId: '1', productId: 'p' }); await Promise.resolve(); });

    expect(mockPost).toHaveBeenCalledTimes(1);
    expect(iap.restore).not.toHaveBeenCalled();
    expect(mockRestoreDeviceAccount).not.toHaveBeenCalled();
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
