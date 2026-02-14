import { describe, it, expect, vi } from 'vitest';
import { MockVpnClient } from '../mock-client';

describe('MockVpnClient', () => {
  it('tracks connect calls', async () => {
    const mock = new MockVpnClient();
    await mock.connect('wg://test1');
    await mock.connect('wg://test2');

    expect(mock.connectCalls).toEqual(['wg://test1', 'wg://test2']);
  });

  it('tracks disconnect calls', async () => {
    const mock = new MockVpnClient();
    await mock.disconnect();
    await mock.disconnect();

    expect(mock.disconnectCalls).toBe(2);
  });

  it('returns settable status', async () => {
    const mock = new MockVpnClient();
    mock.setStatus({ state: 'connected', connectedAt: '2024-01-01' });

    const status = await mock.getStatus();
    expect(status.state).toBe('connected');
    expect(status.connectedAt).toBe('2024-01-01');
  });

  it('returns settable version', async () => {
    const mock = new MockVpnClient();
    mock.setVersion({ version: '1.0.0', go: '1.21', os: 'darwin', arch: 'arm64' });

    const version = await mock.getVersion();
    expect(version.version).toBe('1.0.0');
  });

  it('returns settable ready state', async () => {
    const mock = new MockVpnClient();
    mock.setReady({ ready: true, version: '1.0.0' });

    const ready = await mock.checkReady();
    expect(ready).toEqual({ ready: true, version: '1.0.0' });
  });

  it('simulateEvent pushes events to listeners', () => {
    const mock = new MockVpnClient();
    const listener = vi.fn();

    mock.subscribe(listener);
    mock.simulateEvent({ type: 'state_change', state: 'connected' });

    expect(listener).toHaveBeenCalledWith({ type: 'state_change', state: 'connected' });
  });

  it('unsubscribe removes listener', () => {
    const mock = new MockVpnClient();
    const listener = vi.fn();

    const unsub = mock.subscribe(listener);
    unsub();

    mock.simulateEvent({ type: 'state_change', state: 'connected' });
    expect(listener).not.toHaveBeenCalled();
  });

  it('returns UDID', async () => {
    const mock = new MockVpnClient();
    const udid = await mock.getUDID();
    expect(typeof udid).toBe('string');
    expect(udid.length).toBeGreaterThan(0);
  });

  it('returns config', async () => {
    const mock = new MockVpnClient();
    const config = await mock.getConfig();
    expect(config).toBeDefined();
  });
});
