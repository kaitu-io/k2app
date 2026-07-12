import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as pool from '../entry-pool';
import type { NodeEntry } from '../node-descriptor';
import { EMBEDDED_SEED } from '../antiblock-seed-embedded';

const N = (ip: string): NodeEntry => ({ ip, pin: 'sha256:' + ip, ech: 'E' + ip });

// entry-pool is now a FEEDER + capability flags only. Node storage / ranking /
// health live in the Go RelayManager; the webapp holds no pool and does no
// scoring (single source of truth). addNodes forwards descriptors via
// _k2.run('relay-add-nodes').
describe('entry-pool (relay node feeder + capability flags)', () => {
  beforeEach(() => {
    localStorage.clear();
    pool.__resetRelaySupportForTest();
    pool.__resetSeededForTest();
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { added: 0, total: 0 } }) };
  });
  afterEach(() => { delete (window as any)._k2; });

  it('addNodes forwards descriptors to Go via relay-add-nodes', () => {
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    expect((window as any)._k2.run).toHaveBeenCalledWith('relay-add-nodes', {
      nodes: [N('1.1.1.1'), N('2.2.2.2')],
    });
  });

  it('addNodes is a no-op for an empty list (no IPC)', () => {
    pool.addNodes([]);
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('addNodes is a no-op (no throw) when _k2 is absent', () => {
    delete (window as any)._k2;
    expect(() => pool.addNodes([N('1.1.1.1')])).not.toThrow();
  });

  it('addNodes does not throw if _k2.run rejects (fire-and-forget)', () => {
    (window as any)._k2.run = vi.fn().mockRejectedValue(new Error('bridge down'));
    expect(() => pool.addNodes([N('1.1.1.1')])).not.toThrow();
  });

  it('addNodes skips forwarding once relay is learned unsupported', () => {
    pool.markRelayUnsupported();
    pool.addNodes([N('1.1.1.1')]);
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('relay capability flag: default supported, flips on markRelayUnsupported, resets', () => {
    expect(pool.isRelaySupported()).toBe(true);
    pool.markRelayUnsupported();
    expect(pool.isRelaySupported()).toBe(false);
    pool.__resetRelaySupportForTest();
    expect(pool.isRelaySupported()).toBe(true);
  });

  describe('ensureSeeded (cold-start priming — awaited, once)', () => {
    it('feeds the embedded seed to Go and resolves only after the IPC returns', async () => {
      let resolveRun!: (v: unknown) => void;
      const runMock = vi.fn(() => new Promise((r) => { resolveRun = r as (v: unknown) => void; }));
      (window as any)._k2 = { run: runMock };

      let done = false;
      const p = pool.ensureSeeded().then(() => { done = true; });
      await Promise.resolve();
      expect(done).toBe(false); // still awaiting Go's ingestion
      expect(runMock).toHaveBeenCalledWith('relay-add-nodes', { nodes: EMBEDDED_SEED.nodes });

      resolveRun({ code: 0, message: 'ok', data: { added: EMBEDDED_SEED.nodes.length, total: EMBEDDED_SEED.nodes.length } });
      await p;
      expect(done).toBe(true);
    });

    it('primes at most once (memoized) across calls', async () => {
      const runMock = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { added: 0, total: 3 } });
      (window as any)._k2 = { run: runMock };
      await pool.ensureSeeded();
      await pool.ensureSeeded();
      expect(runMock.mock.calls.filter((c: unknown[]) => c[0] === 'relay-add-nodes')).toHaveLength(1);
    });

    it('is a no-op when relay is unsupported', async () => {
      pool.markRelayUnsupported();
      const runMock = vi.fn();
      (window as any)._k2 = { run: runMock };
      await pool.ensureSeeded();
      expect(runMock).not.toHaveBeenCalled();
    });

    it('does not throw and retries next time when the prime fails', async () => {
      const runMock = vi.fn()
        .mockRejectedValueOnce(new Error('bridge not bound'))
        .mockResolvedValue({ code: 0, message: 'ok', data: { added: 3, total: 3 } });
      (window as any)._k2 = { run: runMock };
      await expect(pool.ensureSeeded()).resolves.toBeUndefined();
      await pool.ensureSeeded(); // first attempt failed → not memoized → retries
      expect(runMock.mock.calls.filter((c: unknown[]) => c[0] === 'relay-add-nodes').length).toBeGreaterThanOrEqual(2);
    });
  });
});
