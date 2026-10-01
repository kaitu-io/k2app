import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { WindowsPanel, IOSPanel } from '../platform-panels';

const openDownloadInNewTab = vi.fn();
vi.mock('@/lib/device-detection', () => ({ openDownloadInNewTab: (...a: unknown[]) => openDownloadInNewTab(...a) }));

describe('install panels funnel', () => {
  let srcs: string[];
  const events = () => srcs.map((s) => new URL(s, 'http://x').searchParams);
  beforeEach(() => {
    srcs = [];
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('download button sends install_click with the platform and still downloads', () => {
    render(<WindowsPanel t={(k) => k} version="1.0.0" isBeta={false} primaryLink="/a.exe" backupLink="/b.exe" />);
    fireEvent.click(screen.getAllByRole('button')[0]);
    expect(events().filter((p) => p.get('e') === 'install_click' && p.get('p') === 'windows')).toHaveLength(1);
    expect(openDownloadInNewTab).toHaveBeenCalledWith('/a.exe');
  });

  it('iOS link sends install_click with p=ios', () => {
    render(<IOSPanel t={(k) => k} version="1.0.0" link="https://apps.example/x" />);
    fireEvent.click(screen.getByRole('link'));
    expect(events().filter((p) => p.get('e') === 'install_click' && p.get('p') === 'ios')).toHaveLength(1);
  });
});
