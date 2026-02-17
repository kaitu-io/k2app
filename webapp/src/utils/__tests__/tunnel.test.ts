import { describe, test, expect } from 'vitest';
import { buildSimpleTunnelURL, parseSimpleTunnelURL } from '../tunnel';

describe('buildSimpleTunnelURL', () => {
  test('builds basic URL with domain and ipv4', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4');
  });

  test('includes port when not default 443', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      port: 8443,
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4&port=8443');
  });

  test('omits port when default 443', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      port: 443,
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4');
  });

  test('includes country parameter', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      country: 'US',
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4&country=US');
  });

  test('includes name as URL fragment', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      name: 'My Tunnel',
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4#My%20Tunnel');
  });

  test('includes ech_config parameter when provided', () => {
    const echConfigListBase64 = 'dGVzdC1lY2gtY29uZmln'; // base64 "test-ech-config"
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      echConfigList: echConfigListBase64,
    });
    expect(url).toContain('ech_config=' + echConfigListBase64);
    expect(url).toBe(`k2wss://example.com?ipv4=1.2.3.4&ech_config=${echConfigListBase64}`);
  });

  test('ech_config is placed before country and name', () => {
    const echConfigListBase64 = 'dGVzdC1lY2gtY29uZmln';
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      echConfigList: echConfigListBase64,
      country: 'US',
      name: 'My Tunnel',
    });
    // Order should be: ipv4, ech_config, country, #name
    const expectedUrl = `k2wss://example.com?ipv4=1.2.3.4&ech_config=${echConfigListBase64}&country=US#My%20Tunnel`;
    expect(url).toBe(expectedUrl);
  });

  test('omits ech_config when empty string', () => {
    const url = buildSimpleTunnelURL({
      domain: 'example.com',
      ipv4: '1.2.3.4',
      echConfigList: '',
    });
    expect(url).toBe('k2wss://example.com?ipv4=1.2.3.4');
    expect(url).not.toContain('ech_config');
  });
});

describe('parseSimpleTunnelURL', () => {
  test('parses basic URL', () => {
    const parsed = parseSimpleTunnelURL('k2wss://example.com?ipv4=1.2.3.4');
    expect(parsed.domain).toBe('example.com');
    expect(parsed.ipv4).toBe('1.2.3.4');
    expect(parsed.port).toBe(443);
  });

  test('parses URL with ech_config parameter', () => {
    const echConfigListBase64 = 'dGVzdC1lY2gtY29uZmln';
    const parsed = parseSimpleTunnelURL(`k2wss://example.com?ipv4=1.2.3.4&ech_config=${echConfigListBase64}`);
    expect(parsed.domain).toBe('example.com');
    expect(parsed.ipv4).toBe('1.2.3.4');
    expect(parsed.echConfigList).toBe(echConfigListBase64);
  });

  test('parses URL with all parameters including ech_config', () => {
    const echConfigListBase64 = 'dGVzdC1lY2gtY29uZmln';
    const url = `k2wss://example.com?ipv4=1.2.3.4&port=8443&ech_config=${echConfigListBase64}&country=US#My%20Tunnel`;
    const parsed = parseSimpleTunnelURL(url);
    expect(parsed.domain).toBe('example.com');
    expect(parsed.ipv4).toBe('1.2.3.4');
    expect(parsed.port).toBe(8443);
    expect(parsed.echConfigList).toBe(echConfigListBase64);
    expect(parsed.country).toBe('US');
    expect(parsed.name).toBe('My Tunnel');
  });
});
