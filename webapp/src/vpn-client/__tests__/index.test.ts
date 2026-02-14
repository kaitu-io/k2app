import { describe, it, expect, beforeEach } from 'vitest';
import { createVpnClient, getVpnClient, resetVpnClient, initVpnClient } from '../index';
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

describe('initVpnClient', () => {
  beforeEach(() => {
    resetVpnClient();
  });

  it('returns HttpVpnClient when not on native platform', async () => {
    const result = await initVpnClient();
    expect(result).toBeInstanceOf(HttpVpnClient);
  });

  it('uses the override client when provided', async () => {
    const mock = new MockVpnClient();
    const result = await initVpnClient(mock);
    expect(result).toBe(mock);
  });

  it('returns same instance on subsequent calls', async () => {
    const first = await initVpnClient();
    const second = await initVpnClient();
    expect(first).toBe(second);
  });

  it('makes instance available via getVpnClient', async () => {
    const mock = new MockVpnClient();
    await initVpnClient(mock);
    expect(getVpnClient()).toBe(mock);
  });
});
