import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as pool from '../entry-pool';
import type { NodeEntry } from '../node-descriptor';

const N = (ip: string): NodeEntry => ({ ip, pin: 'sha256:' + ip, ech: 'E' + ip });

// RELAY_ENABLED=false（真实 flag 值）：feeder 全部 no-op，不发任何 relay-add-nodes IPC。
describe('entry-pool with relay disabled (kill-switch)', () => {
  beforeEach(() => {
    localStorage.clear();
    pool.__resetRelaySupportForTest();
    pool.__resetSeededForTest();
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { added: 0, total: 0 } }) };
  });
  afterEach(() => { delete (window as any)._k2; });

  it('addNodes issues no relay-add-nodes IPC', () => {
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('ensureSeeded resolves immediately with no IPC (startup never blocks on seeding)', async () => {
    await pool.ensureSeeded();
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });
});
