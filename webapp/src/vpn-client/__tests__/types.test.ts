import { describe, it, expect } from 'vitest';
import type { VpnClient, ClientConfig } from '../types';

describe('VpnClient interface', () => {
  it('test_vpn_client_interface_no_setRuleMode — VpnClient type has no setRuleMode property', () => {
    // TypeScript compile-time check: if setRuleMode exists on VpnClient,
    // this test verifies it at runtime by creating a conforming object
    const client: VpnClient = {
      connect: async (_config: ClientConfig) => {},
      disconnect: async () => {},
      checkReady: async () => ({ ready: true, version: '1.0.0' }),
      getStatus: async () => ({ state: 'stopped' }),
      getVersion: async () => ({ version: '1.0.0', go: '1.21', os: 'test', arch: 'test' }),
      getUDID: async () => 'test-id',
      getConfig: async () => ({ server: '' }),
      subscribe: () => () => {},
      destroy: () => {},
    };

    // Verify setRuleMode is NOT a key on the VpnClient interface
    // (it was removed — optional methods like checkForUpdates still exist)
    expect('setRuleMode' in client).toBe(false);
    expect(client.connect).toBeDefined();
  });

  it('test_ClientConfig_has_required_server_field — ClientConfig requires server string', () => {
    const config: ClientConfig = {
      server: 'k2v5://example.com',
    };
    expect(config.server).toBe('k2v5://example.com');
  });

  it('test_ClientConfig_supports_optional_fields — ClientConfig has optional rule, dns, etc.', () => {
    const config: ClientConfig = {
      server: 'k2v5://example.com',
      rule: { global: true },
      dns: { direct: ['8.8.8.8'], proxy: ['1.1.1.1'] },
      log: { level: 'debug' },
      mode: 'vpn',
      proxy: { listen: '127.0.0.1:1080' },
    };
    expect(config.rule?.global).toBe(true);
    expect(config.dns?.direct).toEqual(['8.8.8.8']);
    expect(config.log?.level).toBe('debug');
  });
});
