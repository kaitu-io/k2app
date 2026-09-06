import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { brandConfig } from '../../brands';
import { purchaseSurfaceAvailable } from '../purchase-surface';

function setPlatform(os: string, iap?: object) {
  (window as any)._platform = { os, iap };
}

describe('purchaseSurfaceAvailable', () => {
  beforeEach(() => { delete (window as any)._platform; });
  afterEach(() => { delete (window as any)._platform; });

  it('desktop: available', () => {
    setPlatform('macos');
    expect(purchaseSurfaceAvailable()).toBe(true);
  });

  it('ios without IAP bridge: hidden (Apple 3.1.1)', () => {
    setPlatform('ios');
    expect(purchaseSurfaceAvailable()).toBe(false);
  });

  it('ios with IAP bridge: available', () => {
    setPlatform('ios', {});
    expect(purchaseSurfaceAvailable()).toBe(true);
  });

  it('android follows the brand androidPurchase gate', () => {
    setPlatform('android');
    expect(purchaseSurfaceAvailable()).toBe(brandConfig.features.androidPurchase);
  });

  it('no platform (browser dev): available', () => {
    expect(purchaseSurfaceAvailable()).toBe(true);
  });
});
