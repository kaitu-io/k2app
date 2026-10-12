import { describe, it, expect, vi, afterEach } from 'vitest';
import { fetchServiceBlocker, canOpenServiceSettings, openServiceSettings } from '../service-blocker';

const setPlatform = (p: Record<string, unknown> | undefined) => {
  (window as any)._platform = p;
};

afterEach(() => {
  delete (window as any)._platform;
});

describe('fetchServiceBlocker', () => {
  it('returns the blocker from a v4 shell', async () => {
    const b = { kind: 'background-disallowed', itemName: 'Dev Co' };
    setPlatform({ getServiceBlocker: vi.fn().mockResolvedValue(b) });
    expect(await fetchServiceBlocker()).toEqual(b);
  });

  it.each([
    ['no _platform', undefined],
    ['shell without the method (mobile / pre-v4)', {}],
  ])('%s → null', async (_name, p) => {
    setPlatform(p as any);
    expect(await fetchServiceBlocker()).toBeNull();
  });

  it('rejected invoke (pre-v4 desktop) → null', async () => {
    setPlatform({ getServiceBlocker: vi.fn().mockRejectedValue(new Error('command not found')) });
    expect(await fetchServiceBlocker()).toBeNull();
  });

  it.each([
    ['null', null],
    ['unknown kind', { kind: 'something-new', itemName: 'X' }],
    ['empty name', { kind: 'background-disallowed', itemName: '' }],
  ])('ignores %s', async (_name, value) => {
    setPlatform({ getServiceBlocker: vi.fn().mockResolvedValue(value) });
    expect(await fetchServiceBlocker()).toBeNull();
  });
});

describe('openServiceSettings', () => {
  it('reports capability and calls through', async () => {
    const open = vi.fn().mockResolvedValue(undefined);
    setPlatform({ openServiceSettings: open });
    expect(canOpenServiceSettings()).toBe(true);
    expect(await openServiceSettings()).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
  });

  it('absent or failing → false', async () => {
    setPlatform({});
    expect(canOpenServiceSettings()).toBe(false);
    expect(await openServiceSettings()).toBe(false);
    setPlatform({ openServiceSettings: vi.fn().mockRejectedValue(new Error('x')) });
    expect(await openServiceSettings()).toBe(false);
  });
});
