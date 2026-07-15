import { describe, it, expect } from 'vitest';
import type { TFunction } from 'i18next';
import { ERROR_CODES, getErrorMessage } from '../errorCode';

// t-mock that echoes the key so we can assert routing without loading i18n.
const t = ((key: string) => key) as unknown as TFunction;

describe('brand-split error codes', () => {
  it('403003 BRAND_MISMATCH maps to auth.brandMismatch', () => {
    expect(getErrorMessage(ERROR_CODES.BRAND_MISMATCH, undefined, t)).toBe(
      'auth:auth.brandMismatch'
    );
  });

  it('405001 PAYMENT_CHANNEL_UNAVAILABLE maps to purchase.paymentChannelUnavailable', () => {
    expect(getErrorMessage(ERROR_CODES.PAYMENT_CHANNEL_UNAVAILABLE, undefined, t)).toBe(
      'purchase:purchase.paymentChannelUnavailable'
    );
  });
});
