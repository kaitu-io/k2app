import { describe, test, expect, vi, beforeEach } from 'vitest';
import { classifyApps } from '../classify-apps';
import type { InstalledApp } from '../../types/kaitu-core';

describe('classifyApps', () => {
  beforeEach(() => {
    (globalThis as any).window = (globalThis as any).window ?? {};
    (window as any)._k2 = { run: vi.fn() };
  });

  const apps: InstalledApp[] = [
    { id: 'a', label: 'A', processNames: ['A'] },
    { id: 'b', label: 'B', processNames: ['B'] },
  ];

  test('maps daemon classifications to a Map<id, default>', async () => {
    (window as any)._k2.run.mockResolvedValue({
      code: 0,
      data: { classifications: [
        { id: 'a', default: 'direct', hit_kind: 'app', hit_pattern: 'A*' },
        { id: 'b', default: 'proxy' },
      ] },
    });
    const res = await classifyApps('cn', apps);
    expect(res.get('a')).toBe('direct');
    expect(res.get('b')).toBe('proxy');
    expect((window as any)._k2.run).toHaveBeenCalledWith('classify-apps', {
      region: 'cn',
      installed: [
        { id: 'a', label: 'A', installer_package_name: '', process_names: ['A'] },
        { id: 'b', label: 'B', installer_package_name: '', process_names: ['B'] },
      ],
    });
  });

  test('empty region → all proxy without calling daemon', async () => {
    const res = await classifyApps('', apps);
    expect(res.get('a')).toBe('proxy');
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  test('daemon error → all proxy (fail-soft)', async () => {
    (window as any)._k2.run.mockResolvedValue({ code: 500, message: 'boom' });
    const res = await classifyApps('cn', apps);
    expect(res.get('a')).toBe('proxy');
    expect(res.get('b')).toBe('proxy');
  });
});
