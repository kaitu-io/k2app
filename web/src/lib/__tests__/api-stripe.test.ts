import { describe, it, expect, vi, afterEach } from 'vitest';
import { api } from '../api';

describe('api stripe methods (overleap)', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('createStripeCheckout POSTs plan pid and returns url', async () => {
    const spy = vi
      .spyOn(api, 'request')
      .mockResolvedValue({ url: 'https://checkout.stripe.com/c/pay/x' });
    const res = await api.createStripeCheckout('overleap-basic-1y');
    expect(spy).toHaveBeenCalledWith(
      '/api/user/stripe/checkout',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ plan: 'overleap-basic-1y' }),
      })
    );
    expect(res.url).toContain('checkout.stripe.com');
  });

  it('createStripePortal POSTs and returns url', async () => {
    const spy = vi
      .spyOn(api, 'request')
      .mockResolvedValue({ url: 'https://billing.stripe.com/p/session/x' });
    const res = await api.createStripePortal();
    expect(spy).toHaveBeenCalledWith(
      '/api/user/stripe/portal',
      expect.objectContaining({ method: 'POST' })
    );
    expect(res.url).toContain('billing.stripe.com');
  });
});
