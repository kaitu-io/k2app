import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useStripeCheckout } from '../useStripeCheckout';
import { cloudApi } from '../../services/cloud-api';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: vi.fn() },
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

const postMock = vi.mocked(cloudApi.post);

describe('useStripeCheckout', () => {
  beforeEach(() => {
    // vi.clearAllMocks 会清实现——beforeEach 里重设（项目已知 gotcha）
    postMock.mockReset();
    window._platform = { openExternal: vi.fn() } as any;
  });

  it('checkout posts plan pid and opens the returned url', async () => {
    postMock.mockResolvedValue({ code: 0, data: { url: 'https://checkout.stripe.com/x' } } as any);
    const { result } = renderHook(() => useStripeCheckout());
    let ok = false;
    await act(async () => { ok = await result.current.checkout('ol-basic-1y'); });
    expect(ok).toBe(true);
    expect(postMock).toHaveBeenCalledWith('/api/user/stripe/checkout', { plan: 'ol-basic-1y' });
    expect(window._platform!.openExternal).toHaveBeenCalledWith('https://checkout.stripe.com/x');
    expect(result.current.error).toBeNull();
  });

  it('checkout failure surfaces error and opens nothing', async () => {
    postMock.mockResolvedValue({ code: 405001, message: 'unavailable' } as any);
    const { result } = renderHook(() => useStripeCheckout());
    let ok = true;
    await act(async () => { ok = await result.current.checkout('p'); });
    expect(ok).toBe(false);
    expect(window._platform!.openExternal).not.toHaveBeenCalled();
    expect(result.current.error).not.toBeNull();
  });

  it('openPortal posts and opens portal url', async () => {
    postMock.mockResolvedValue({ code: 0, data: { url: 'https://billing.stripe.com/p' } } as any);
    const { result } = renderHook(() => useStripeCheckout());
    await act(async () => { await result.current.openPortal(); });
    expect(postMock).toHaveBeenCalledWith('/api/user/stripe/portal', {});
    expect(window._platform!.openExternal).toHaveBeenCalledWith('https://billing.stripe.com/p');
  });
});
