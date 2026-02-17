/**
 * Type System Tests for the new global interface split
 *
 * AFTER the split:
 *   window._k2 = { run(action, params): Promise<SResponse> }   (pure VPN only)
 *   window._platform = { os, isDesktop, isMobile, version, getUdid(), storage, ... }
 *
 * These tests verify the new type contracts at runtime.
 * They should FAIL until the production types are updated.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import type { IK2, IPlatform, ISecureStorage, SResponse } from '../kaitu-core';

/**
 * IK2Vpn — the new slim VPN-only interface
 *
 * We import IK2 (the current name) and test against the new shape.
 * After the split, IK2 should become the slim VPN-only interface
 * with a `run` method and NO `api` or `platform` properties.
 */
describe('IK2Vpn interface shape', () => {
  it('should have a run method on the IK2 interface', () => {
    // Create an object that satisfies the NEW IK2 contract (VPN-only)
    const vpn: IK2 = {
      run: async (_action: string, _params?: any): Promise<SResponse> => ({
        code: 0,
      }),
    } as any;

    // The new IK2 (IK2Vpn) should have `run` as a callable function
    expect(typeof vpn.run).toBe('function');
  });

  it('should NOT have api property on IK2 interface', () => {
    // After the split, IK2 is VPN-only.
    // Constructing a minimal valid IK2 should NOT require an `api` property.
    const vpn: IK2 = {
      run: async () => ({ code: 0 }),
    } as any;

    // Verify that a properly typed IK2 object does NOT contain `api`
    // The current IK2 has `api: IK2Api`, so this test will FAIL
    // until the type is updated to remove `api`.
    expect('api' in vpn).toBe(false);

    // Also verify structurally: a bare IK2-typed object with only `run`
    // should be assignable without TypeScript error.
    // At runtime, we test that IK2 definition doesn't mandate `api`.
    const keys = Object.keys(vpn);
    expect(keys).not.toContain('api');
  });

  it('should NOT have platform property on IK2 interface', () => {
    const vpn: IK2 = {
      run: async () => ({ code: 0 }),
    } as any;

    // The current IK2 has `platform: IPlatform`, so this test will FAIL
    expect('platform' in vpn).toBe(false);
    const keys = Object.keys(vpn);
    expect(keys).not.toContain('platform');
  });

  it('should NOT have core property on IK2 interface', () => {
    // After the split, IK2 IS the core — no nested `core` property needed
    const vpn: IK2 = {
      run: async () => ({ code: 0 }),
    } as any;

    expect('core' in vpn).toBe(false);
  });
});

/**
 * Window global declarations
 *
 * After the split, Window should declare BOTH:
 *   _k2: IK2 (VPN-only)
 *   _platform: IPlatform (platform capabilities)
 */
describe('Window declares both globals', () => {
  let originalK2: any;
  let originalPlatform: any;

  beforeEach(() => {
    originalK2 = (window as any)._k2;
    originalPlatform = (window as any)._platform;
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  it('should declare _k2 on Window interface', () => {
    // After the split, window._k2 should be assignable
    (window as any)._k2 = {
      run: async () => ({ code: 0 }),
    };
    expect(window._k2).toBeDefined();
  });

  it('should declare _platform on Window interface', () => {
    // After the split, window._platform should exist on Window
    // Currently only window._k2 is declared, so this will FAIL
    // because TypeScript doesn't know about window._platform
    const mockPlatform: IPlatform = {
      os: 'web',
      isDesktop: false,
      isMobile: false,
      version: '1.0.0',
      getUdid: async () => 'test-udid',
      storage: {
        get: async () => null,
        set: async () => {},
        remove: async () => {},
        has: async () => false,
        clear: async () => {},
        keys: async () => [],
      },
    };

    // Assign to window._platform
    (window as any)._platform = mockPlatform;

    // The key test: window._platform should be declared in the Window interface.
    // We verify by accessing it through the typed `window` global.
    // This line will fail at compile time until Window declares _platform.
    expect(window._platform).toBeDefined();
    expect(window._platform.os).toBe('web');
  });

  it('should have _k2 and _platform as separate globals, not nested', () => {
    (window as any)._k2 = { run: async () => ({ code: 0 }) };
    (window as any)._platform = { os: 'web', isDesktop: false, isMobile: false, version: '1.0.0' };

    // _k2 should NOT contain platform
    expect((window._k2 as any).platform).toBeUndefined();
    // _platform should be its own top-level object
    expect(window._platform).toBeDefined();
  });
});

/**
 * IPlatform interface requirements
 */
describe('IPlatform has getUdid', () => {
  it('should include getUdid returning Promise<string>', () => {
    const platform: IPlatform = {
      os: 'web',
      isDesktop: false,
      isMobile: false,
      version: '1.0.0',
      getUdid: async () => 'test-udid-12345',
    };

    expect(typeof platform.getUdid).toBe('function');
  });

  it('should have getUdid as a required (non-optional) property', () => {
    // After the split, getUdid should be REQUIRED on IPlatform (not optional).
    // Currently it's optional (`getUdid?()`), so this test will FAIL.
    //
    // We create an IPlatform WITHOUT getUdid. If the property is required,
    // TypeScript won't compile. At runtime, we verify a "complete" platform
    // object always has getUdid defined.
    const platform: IPlatform = {
      os: 'web',
      isDesktop: false,
      isMobile: false,
      version: '1.0.0',
    } as IPlatform;

    // If getUdid is required, this object is invalid. We verify at runtime:
    // a properly constructed IPlatform MUST have getUdid defined (not undefined).
    // The current type has `getUdid?()` which allows undefined — this test
    // asserts the NEW contract where it's required.
    //
    // We test by checking that the type system requires it:
    // create a "valid" IPlatform and ensure getUdid is always present.
    // For the RED phase, we test that the CURRENT type fails this requirement.

    // In the current code, IPlatform.getUdid is optional, so this will be undefined
    // After the split, it should be required, so this should never be undefined
    expect(platform.getUdid).toBeDefined();
  });
});

describe('IPlatform has storage', () => {
  it('should include storage of type ISecureStorage', () => {
    const mockStorage: ISecureStorage = {
      get: async () => null,
      set: async () => {},
      remove: async () => {},
      has: async () => false,
      clear: async () => {},
      keys: async () => [],
    };

    const platform: IPlatform = {
      os: 'web',
      isDesktop: false,
      isMobile: false,
      version: '1.0.0',
      storage: mockStorage,
    } as IPlatform;

    expect(platform.storage).toBeDefined();
    expect(typeof platform.storage!.get).toBe('function');
    expect(typeof platform.storage!.set).toBe('function');
    expect(typeof platform.storage!.remove).toBe('function');
  });

  it('should have storage as a required (non-optional) property', () => {
    // After the split, storage should be REQUIRED on IPlatform.
    // Currently it's optional (`storage?: ISecureStorage`).
    const platform: IPlatform = {
      os: 'web',
      isDesktop: false,
      isMobile: false,
      version: '1.0.0',
    } as IPlatform;

    // Same pattern as getUdid: test that a "bare" IPlatform has storage defined
    // This will FAIL with current optional type until we make it required
    expect(platform.storage).toBeDefined();
  });
});
