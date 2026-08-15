import { describe, it, expect, beforeEach, vi } from 'vitest';

const invokeMock = vi.fn();
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => invokeMock(...args),
}));
vi.mock('@tauri-apps/api/event', () => ({
  listen: vi.fn(async () => () => {}),
}));
vi.mock('@tauri-apps/plugin-opener', () => ({ openUrl: vi.fn() }));
vi.mock('@tauri-apps/plugin-clipboard-manager', () => ({
  writeText: vi.fn(),
  readText: vi.fn(),
}));
vi.mock('@tauri-apps/plugin-log', () => ({
  debug: vi.fn(async () => {}),
  info: vi.fn(async () => {}),
  warn: vi.fn(async () => {}),
  error: vi.fn(async () => {}),
}));

import { injectTauriGlobals } from '../tauri-k2';

function defaultInvokeImpl(cmd: string): unknown {
  switch (cmd) {
    case 'get_platform_info':
      return { os: 'macos', version: '0.4.8', arch: 'aarch64' };
    case 'get_update_channel':
      return 'stable';
    case 'get_update_status':
      return null;
    default:
      return null;
  }
}

describe('tauri-k2 ui_boot_ok handshake', () => {
  beforeEach(() => {
    localStorage.clear();
    invokeMock.mockReset();
    invokeMock.mockImplementation(async (cmd: string) => defaultInvokeImpl(cmd));
  });

  it('calls ui_boot_ok during bridge init', async () => {
    await injectTauriGlobals();
    const commands = invokeMock.mock.calls.map((c) => c[0]);
    expect(commands).toContain('ui_boot_ok');
    // and still shows the window afterwards
    expect(commands).toContain('show_window');
  });

  it('survives an old shell without the command', async () => {
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'ui_boot_ok') throw new Error('command ui_boot_ok not found');
      return defaultInvokeImpl(cmd);
    });
    await expect(injectTauriGlobals()).resolves.toBeUndefined();
  });
});
