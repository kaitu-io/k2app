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
});
