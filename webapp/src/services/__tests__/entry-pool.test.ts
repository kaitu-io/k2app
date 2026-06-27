import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import * as pool from '../entry-pool';
import type { NodeEntry } from '../node-descriptor';

const N = (ip: string): NodeEntry => ({ ip, pin: 'sha256:' + ip, ech: 'E' + ip });

describe('entry-pool', () => {
  beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); vi.setSystemTime(new Date('2026-06-25T00:00:00Z')); });
  afterEach(() => { vi.useRealTimers(); });

  it('addNodes then getNodes returns them (deduped, persisted)', () => {
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2'), N('1.1.1.1')]);
    const got = pool.getNodes();
    expect(got.map(n => n.ip).sort()).toEqual(['1.1.1.1', '2.2.2.2']);
    // persisted: a fresh read (module state is the same module, but localStorage is the source) still has them
    expect(localStorage.getItem('k2_node_pool')).toBeTruthy();
  });

  it('recordSuccess raises a node above an untouched peer in sort order', () => {
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    pool.recordSuccess('2.2.2.2');
    expect(pool.getNodes()[0].ip).toBe('2.2.2.2');
  });

  it('recordFailure sinks a node below peers', () => {
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    pool.recordFailure('1.1.1.1');
    expect(pool.getNodes()[pool.getNodes().length - 1].ip).toBe('1.1.1.1');
  });

  it('caps the pool at 64 nodes, dropping the lowest-scored', () => {
    const many: NodeEntry[] = [];
    for (let i = 0; i < 70; i++) many.push(N('10.0.0.' + i));
    pool.addNodes(many);
    pool.recordFailure('10.0.0.0'); // make this one the worst
    pool.addNodes([N('10.0.0.100')]);
    const ips = pool.getNodes().map(n => n.ip);
    expect(ips.length).toBeLessThanOrEqual(64);
    expect(ips).not.toContain('10.0.0.0'); // worst was evicted
  });

  it('prunes nodes with no success in 7 days', () => {
    pool.addNodes([N('1.1.1.1')]);
    pool.recordSuccess('1.1.1.1'); // lastOkAt = now
    vi.setSystemTime(new Date('2026-07-03T00:01:00Z')); // > 7 days later
    pool.addNodes([N('2.2.2.2')]); // triggers prune on save
    expect(pool.getNodes().map(n => n.ip)).toEqual(['2.2.2.2']);
  });

  it('sticky direct-blocked marker honours a 5-minute TTL', () => {
    expect(pool.isDirectBlocked()).toBe(false);
    pool.markDirectBlocked();
    expect(pool.isDirectBlocked()).toBe(true);
    vi.advanceTimersByTime(5 * 60 * 1000 + 1);
    expect(pool.isDirectBlocked()).toBe(false);
  });

  it('clearDirectBlocked removes the marker immediately', () => {
    pool.markDirectBlocked();
    pool.clearDirectBlocked();
    expect(pool.isDirectBlocked()).toBe(false);
  });

  it('tolerates corrupt localStorage without throwing', () => {
    localStorage.setItem('k2_node_pool', '{not json');
    expect(() => pool.getNodes()).not.toThrow();
    expect(pool.getNodes()).toEqual([]);
  });
});
