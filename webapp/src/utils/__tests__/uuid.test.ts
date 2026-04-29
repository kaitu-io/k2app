import { describe, it, expect, vi, afterEach } from 'vitest';
import { randomUUID } from '../uuid';

const UUID_V4_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe('randomUUID', () => {
  const originalRandomUUID = crypto.randomUUID;

  afterEach(() => {
    Object.defineProperty(crypto, 'randomUUID', {
      value: originalRandomUUID,
      configurable: true,
      writable: true,
    });
  });

  it('returns valid v4 UUID via native crypto.randomUUID when available', () => {
    const native = vi.fn(() => '11111111-2222-4333-8444-555555555555' as `${string}-${string}-${string}-${string}-${string}`);
    Object.defineProperty(crypto, 'randomUUID', { value: native, configurable: true, writable: true });

    const id = randomUUID();

    expect(native).toHaveBeenCalledOnce();
    expect(id).toBe('11111111-2222-4333-8444-555555555555');
  });

  it('falls back to getRandomValues when crypto.randomUUID is missing (Huawei old WebView)', () => {
    Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true, writable: true });

    const id = randomUUID();

    expect(id).toMatch(UUID_V4_RE);
  });

  it('produces distinct ids on repeated polyfill calls', () => {
    Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true, writable: true });

    const ids = new Set(Array.from({ length: 50 }, () => randomUUID()));

    expect(ids.size).toBe(50);
    for (const id of ids) expect(id).toMatch(UUID_V4_RE);
  });
});
