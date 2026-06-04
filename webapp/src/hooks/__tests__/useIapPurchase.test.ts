import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';

// ---- Mocks ----------------------------------------------------------------

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

const mockPost = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    post: (...args: any[]) => mockPost(...args),
  },
}));

const mockCacheSet = vi.fn();
vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    set: (...args: any[]) => mockCacheSet(...args),
  },
}));

import { useIapPurchase } from '../useIapPurchase';

// ---- IAP test double ------------------------------------------------------

function makeIap(overrides: Record<string, any> = {}) {
  return {
    getProducts: vi.fn().mockResolvedValue([]),
    purchase: vi.fn(),
    restore: vi.fn(),
    finishTransaction: vi.fn().mockResolvedValue(undefined),
    onTransactionUpdate: vi.fn().mockReturnValue(() => {}),
    ...overrides,
  };
}

const GRANTED_USER = { uuid: 'u1', expiredAt: 9999999999 };

describe('useIapPurchase', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    mockPost.mockResolvedValue({ code: 0, data: GRANTED_USER });
    mockCacheSet.mockReset();
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
    vi.clearAllMocks();
  });

  it('purchase success: verifies with {transactionId}, finishes, sets lastGrantedUser', async () => {
    const iap = makeIap({
      purchase: vi.fn().mockResolvedValue({ result: 'success', transactionId: 'tx-1' }),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.purchase('io.kaitu.sub.basic.1y', 'acct-tok');
    });

    expect(mockPost).toHaveBeenCalledWith('/api/user/apple-iap/verify', { transactionId: 'tx-1' });
    expect(iap.finishTransaction).toHaveBeenCalledWith('tx-1');
    await waitFor(() => expect(result.current.lastGrantedUser).toEqual(GRANTED_USER));
    expect(mockCacheSet).toHaveBeenCalledWith('api:user_info', GRANTED_USER, { ttl: 3600 });
    expect(result.current.purchasing).toBe(false);
  });

  it('purchase cancelled: does NOT verify, no error', async () => {
    const iap = makeIap({
      purchase: vi.fn().mockResolvedValue({ result: 'cancelled' }),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.purchase('io.kaitu.sub.basic.1y', 'acct-tok');
    });

    expect(mockPost).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBeNull();
    expect(result.current.purchasing).toBe(false);
  });

  it('purchase pending: does NOT verify, sets pendingApproval error', async () => {
    const iap = makeIap({
      purchase: vi.fn().mockResolvedValue({ result: 'pending' }),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.purchase('io.kaitu.sub.basic.1y', 'acct-tok');
    });

    expect(mockPost).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('purchase:purchase.iap.pendingApproval');
  });

  it('verify-fail: does NOT finishTransaction (the critical guard)', async () => {
    mockPost.mockResolvedValue({ code: 500, message: 'boom' });
    const iap = makeIap({
      purchase: vi.fn().mockResolvedValue({ result: 'success', transactionId: 'tx-2' }),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.purchase('io.kaitu.sub.basic.1y', 'acct-tok');
    });

    expect(mockPost).toHaveBeenCalledWith('/api/user/apple-iap/verify', { transactionId: 'tx-2' });
    expect(iap.finishTransaction).not.toHaveBeenCalled();
    expect(result.current.lastGrantedUser).toBeNull();
    expect(result.current.purchaseError).toBe('purchase:purchase.iap.verifyFailed');
  });

  it('finish-throws: grant still set, no error surfaced', async () => {
    const iap = makeIap({
      purchase: vi.fn().mockResolvedValue({ result: 'success', transactionId: 'tx-3' }),
      finishTransaction: vi.fn().mockRejectedValue(new Error('finish boom')),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.purchase('io.kaitu.sub.basic.1y', 'acct-tok');
    });

    await waitFor(() => expect(result.current.lastGrantedUser).toEqual(GRANTED_USER));
    expect(result.current.purchaseError).toBeNull();
  });

  it('restore empty: sets nothingToRestore', async () => {
    const iap = makeIap({
      restore: vi.fn().mockResolvedValue([]),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.restore();
    });

    expect(mockPost).not.toHaveBeenCalled();
    expect(result.current.purchaseError).toBe('purchase:purchase.iap.nothingToRestore');
    expect(result.current.restoring).toBe(false);
  });

  it('restore multiple: verifies N times sequentially', async () => {
    const iap = makeIap({
      restore: vi.fn().mockResolvedValue([
        { transactionId: 'r-1', productId: 'p1' },
        { transactionId: 'r-2', productId: 'p2' },
        { transactionId: 'r-3', productId: 'p3' },
      ]),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    await act(async () => {
      await result.current.restore();
    });

    expect(mockPost).toHaveBeenCalledTimes(3);
    expect(mockPost).toHaveBeenNthCalledWith(1, '/api/user/apple-iap/verify', { transactionId: 'r-1' });
    expect(mockPost).toHaveBeenNthCalledWith(2, '/api/user/apple-iap/verify', { transactionId: 'r-2' });
    expect(mockPost).toHaveBeenNthCalledWith(3, '/api/user/apple-iap/verify', { transactionId: 'r-3' });
    expect(iap.finishTransaction).toHaveBeenCalledTimes(3);
  });

  it('onTransactionUpdate: captured cb triggers verify path', async () => {
    let captured: ((d: { transactionId: string; productId: string }) => void) | null = null;
    const iap = makeIap({
      onTransactionUpdate: vi.fn((cb: any) => {
        captured = cb;
        return () => {};
      }),
    });
    (window as any)._platform = { iap };

    const { result } = renderHook(() => useIapPurchase());
    expect(iap.onTransactionUpdate).toHaveBeenCalled();
    expect(captured).toBeTypeOf('function');

    await act(async () => {
      captured!({ transactionId: 'bg-1', productId: 'p1' });
    });

    expect(mockPost).toHaveBeenCalledWith('/api/user/apple-iap/verify', { transactionId: 'bg-1' });
    expect(iap.finishTransaction).toHaveBeenCalledWith('bg-1');
    await waitFor(() => expect(result.current.lastGrantedUser).toEqual(GRANTED_USER));
  });
});
