import { describe, it, expect, beforeEach } from 'vitest';
import { useVpnStore } from '../vpn.store';
import { createVpnClient, resetVpnClient } from '../../vpn-client';
import { MockVpnClient } from '../../vpn-client/mock-client';

describe('useVpnStore', () => {
  let mock: MockVpnClient;

  beforeEach(() => {
    resetVpnClient();
    mock = new MockVpnClient();
    createVpnClient(mock);

    // Reset zustand store
    useVpnStore.setState({
      state: 'stopped',
      ready: null,
      error: null,
    });
  });

  describe('init', () => {
    it('sets ready state from client', async () => {
      mock.setReady({ ready: true, version: '1.0.0' });
      mock.setStatus({ state: 'stopped' });

      await useVpnStore.getState().init();

      expect(useVpnStore.getState().ready).toEqual({ ready: true, version: '1.0.0' });
    });

    it('sets vpn state from status when ready', async () => {
      mock.setReady({ ready: true, version: '1.0.0' });
      mock.setStatus({ state: 'connected', connectedAt: '2024-01-01' });

      await useVpnStore.getState().init();

      expect(useVpnStore.getState().state).toBe('connected');
    });

    it('does not fetch status when not ready', async () => {
      mock.setReady({ ready: false, reason: 'not_running' });

      await useVpnStore.getState().init();

      expect(useVpnStore.getState().ready).toEqual({ ready: false, reason: 'not_running' });
      expect(useVpnStore.getState().state).toBe('stopped');
    });
  });

  describe('connect', () => {
    it('test_vpn_store_connect_with_config — passes ClientConfig to vpnClient', async () => {
      const config = { server: 'k2v5://test', rule: { global: true } };
      await useVpnStore.getState().connect(config);

      expect(mock.connectCalls).toEqual([config]);
      // state was set to 'connecting' at start
    });

    it('reverts to stopped and sets error on failure', async () => {
      mock.setConnectError(new Error('Connection refused'));

      await useVpnStore.getState().connect({ server: 'k2v5://test' });

      expect(useVpnStore.getState().state).toBe('stopped');
      expect(useVpnStore.getState().error).toBe('Connection refused');
    });
  });

  describe('disconnect', () => {
    it('calls disconnect without changing state', async () => {
      await useVpnStore.getState().disconnect();

      expect(mock.disconnectCalls).toBe(1);
      expect(useVpnStore.getState().state).toBe('stopped');
    });

    it('sets error on failure', async () => {
      mock.setDisconnectError(new Error('Disconnect failed'));

      await useVpnStore.getState().disconnect();

      expect(useVpnStore.getState().error).toBe('Disconnect failed');
    });
  });

  describe('event subscription', () => {
    it('updates state on state_change event', async () => {
      mock.setReady({ ready: true, version: '1.0.0' });
      mock.setStatus({ state: 'stopped' });

      await useVpnStore.getState().init();

      mock.simulateEvent({ type: 'state_change', state: 'connected' });

      expect(useVpnStore.getState().state).toBe('connected');
      expect(useVpnStore.getState().error).toBeNull();
    });

    it('updates error on error event', async () => {
      mock.setReady({ ready: true, version: '1.0.0' });
      mock.setStatus({ state: 'stopped' });

      await useVpnStore.getState().init();

      mock.simulateEvent({ type: 'error', message: 'Something went wrong' });

      expect(useVpnStore.getState().error).toBe('Something went wrong');
    });
  });
});
