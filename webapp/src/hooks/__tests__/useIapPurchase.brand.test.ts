import { describe, it, expect } from 'vitest';
import { IAP_PRODUCT_IDS } from '../useIapPurchase';
import { brandConfig } from '../../brands';

describe('IAP_PRODUCT_IDS brand derivation', () => {
  it('derives from the active brand config', () => {
    expect(IAP_PRODUCT_IDS).toEqual(brandConfig.iapProductIds);
  });
  it('active brand product ids carry the brand bundle prefix', () => {
    const prefix = brandConfig.id === 'overleap' ? 'io.overleap.' : 'io.kaitu.';
    for (const id of IAP_PRODUCT_IDS) expect(id.startsWith(prefix)).toBe(true);
  });
});
