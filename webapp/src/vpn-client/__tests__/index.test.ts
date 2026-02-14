import { describe, it, expect, beforeEach } from 'vitest';
import { createVpnClient, getVpnClient, resetVpnClient } from '../index';
import { MockVpnClient } from '../mock-client';
import { HttpVpnClient } from '../http-client';

describe('VpnClient factory', () => {
  beforeEach(() => {
    resetVpnClient();
  });

  it('createVpnClient with override returns the override', () => {
    const mock = new MockVpnClient();
    const result = createVpnClient(mock);
    expect(result).toBe(mock);
  });

  it('createVpnClient without override returns HttpVpnClient', () => {
    const result = createVpnClient();
    expect(result).toBeInstanceOf(HttpVpnClient);
  });

  it('createVpnClient returns same instance on subsequent calls', () => {
    const first = createVpnClient();
    const second = createVpnClient();
    expect(first).toBe(second);
  });

  it('getVpnClient throws if not initialized', () => {
    expect(() => getVpnClient()).toThrow('VpnClient not initialized');
  });

  it('getVpnClient returns instance after createVpnClient', () => {
    const mock = new MockVpnClient();
    createVpnClient(mock);
    expect(getVpnClient()).toBe(mock);
  });

  it('resetVpnClient clears the instance', () => {
    createVpnClient();
    resetVpnClient();
    expect(() => getVpnClient()).toThrow('VpnClient not initialized');
  });
});
