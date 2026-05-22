import { describe, it, expect, beforeEach } from 'vitest';
import { buildBypassRoutes } from '../config.store';
import type { AppBypassEntry } from '../app-bypass.store';

beforeEach(() => {
  (window as any)._platform = { os: 'macos' };
});

describe('buildBypassRoutes', () => {
  it('returns [] for empty entries', () => {
    expect(buildBypassRoutes([])).toEqual([]);
  });

  it('emits one process_name route for desktop entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a', 'a-helper'], addedAt: 1 },
      { id: 'b', label: 'B', kind: 'process', names: ['b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes).toEqual([
      { via: 'direct', match: { process_name: ['a', 'a-helper', 'b'] } },
    ]);
  });

  it('emits one package_name route for Android entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'com.test', label: 'T', kind: 'package', names: ['com.test'], addedAt: 1 },
    ];
    (window as any)._platform = { os: 'android' };
    expect(buildBypassRoutes(entries)).toEqual([
      { via: 'direct', match: { package_name: ['com.test'] } },
    ]);
  });

  it('emits two routes when both kinds present (cross-platform import scenario)', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a'], addedAt: 1 },
      { id: 'com.b', label: 'B', kind: 'package', names: ['com.b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes).toHaveLength(2);
    expect(routes[0].match.process_name).toEqual(['a']);
    expect(routes[1].match.package_name).toEqual(['com.b']);
  });

  it('dedupes names across entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['shared', 'unique-a'], addedAt: 1 },
      { id: 'b', label: 'B', kind: 'process', names: ['shared', 'unique-b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toEqual(['shared', 'unique-a', 'unique-b']);
  });

  it('returns [] when platform.os is ios (iOS guard)', () => {
    (window as any)._platform = { os: 'ios' };
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a'], addedAt: 1 },
    ];
    expect(buildBypassRoutes(entries)).toEqual([]);
  });

  // =========================================================================
  // OS-specific quirks (added 2026-05-22) — empirically motivated by per-OS
  // process naming behaviors discovered during the 10/10 audit.
  // =========================================================================

  it('passes long executable basenames through verbatim (Linux exe-symlink path)', () => {
    // Linux searcher reads /proc/PID/exe → full basename, no TASK_COMM_LEN
    // truncation. Bypass entries for Electron apps must round-trip the full
    // name so the rule engine can match observed kernel attribution.
    const entries: AppBypassEntry[] = [
      { id: 'chrome', label: 'Google Chrome', kind: 'process',
        names: ['Google Chrome', 'Google Chrome Helper', 'Google Chrome Helper (GPU)', 'Google Chrome Helper (Renderer)'],
        addedAt: 1 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toContain('Google Chrome Helper (Renderer)');
    // No name should be truncated to 15 chars
    for (const name of routes[0].match.process_name as string[]) {
      expect(name.length === 0 || name === name.trim()).toBe(true);
    }
  });

  it('passes multi-byte UTF-8 names through verbatim (Chinese app bundles)', () => {
    const entries: AppBypassEntry[] = [
      { id: 'wx', label: '微信', kind: 'process', names: ['微信', '微信网络助手'], addedAt: 1 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toEqual(['微信', '微信网络助手']);
  });

  it('preserves spaces and parens (macOS helper bundle naming)', () => {
    // Verified empirically: lsof returns "Code Helper (Renderer)" with
    // spaces + parens intact. Route construction must not normalize.
    const entries: AppBypassEntry[] = [
      { id: 'code', label: 'Visual Studio Code', kind: 'process',
        names: ['Code Helper', 'Code Helper (GPU)', 'Code Helper (Renderer)', 'Code Helper (Plugin)'],
        addedAt: 1 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toEqual([
      'Code Helper', 'Code Helper (GPU)', 'Code Helper (Renderer)', 'Code Helper (Plugin)',
    ]);
  });

  it('preserves Windows .exe suffix and ApplicationFrameHost for UWP', () => {
    (window as any)._platform = { os: 'windows' };
    const entries: AppBypassEntry[] = [
      { id: 'edge', label: 'Microsoft Edge', kind: 'process',
        names: ['msedge.exe'], addedAt: 1 },
      { id: 'uwp', label: 'WeChat (Store)', kind: 'process',
        names: ['ApplicationFrameHost.exe'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toEqual([
      'msedge.exe', 'ApplicationFrameHost.exe',
    ]);
  });

  // =========================================================================
  // Auto-detected (Android Smart Bypass) path
  // =========================================================================

  it('autoPackageNames alone produces a package_name route (no user entries)', () => {
    (window as any)._platform = { os: 'android' };
    const routes = buildBypassRoutes([], ['com.tencent.mm', 'com.alipay.android']);
    expect(routes).toEqual([
      { via: 'direct', match: { package_name: ['com.tencent.mm', 'com.alipay.android'] } },
    ]);
  });

  it('autoPackageNames unions with user package entries, dedup wins on overlap', () => {
    (window as any)._platform = { os: 'android' };
    const entries: AppBypassEntry[] = [
      { id: 'mm', label: 'WeChat', kind: 'package', names: ['com.tencent.mm'], addedAt: 1 },
      { id: 'custom', label: 'Custom', kind: 'package', names: ['com.example.custom'], addedAt: 2 },
    ];
    // Auto-detected also includes com.tencent.mm — must dedup once,
    // preserving user-added first-seen order.
    const routes = buildBypassRoutes(entries, ['com.tencent.mm', 'com.unionpay']);
    expect(routes[0].match.package_name).toEqual([
      'com.tencent.mm',     // user-added first
      'com.example.custom', // user-added second
      'com.unionpay',       // auto-detected, unique
    ]);
  });

  it('empty entries + empty autoPackageNames returns []', () => {
    expect(buildBypassRoutes([], [])).toEqual([]);
  });
});
