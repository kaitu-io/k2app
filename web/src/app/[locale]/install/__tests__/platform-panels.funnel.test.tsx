import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { WindowsPanel, MacOSPanel, AndroidPanel, IOSPanel } from '../platform-panels';

const openDownloadInNewTab = vi.fn();
vi.mock('@/lib/device-detection', () => ({ openDownloadInNewTab: (...a: unknown[]) => openDownloadInNewTab(...a) }));

describe('install panels funnel', () => {
  let srcs: string[];
  const events = () => srcs.map((s) => new URL(s, 'http://x').searchParams);
  const clicks = () => events().filter((p) => p.get('e') === 'install_click').map((p) => `${p.get('p')}/${p.get('s')}`);
  beforeEach(() => {
    srcs = [];
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('download button sends install_click with the platform and s=button, and still downloads', () => {
    render(<WindowsPanel t={(k) => k} version="1.0.0" isBeta={false} primaryLink="/a.exe" backupLink="/b.exe" />);
    fireEvent.click(screen.getAllByRole('button')[0]);
    expect(clicks()).toEqual(['windows/button']);
    expect(openDownloadInNewTab).toHaveBeenCalledWith('/a.exe');
  });

  it('iOS link sends install_click with p=ios, s=button', () => {
    render(<IOSPanel t={(k) => k} version="1.0.0" link="https://apps.example/x" />);
    fireEvent.click(screen.getByRole('link'));
    expect(clicks()).toEqual(['ios/button']);
  });

  it.each([
    ['windows', <WindowsPanel key="w" t={(k) => k} version="1.0.0" isBeta={false} primaryLink="/a.exe" backupLink="/b.exe" />],
    ['macos', <MacOSPanel key="m" t={(k) => k} version="1.0.0" isBeta={false} primaryLink="/a.pkg" backupLink="/b.pkg" onCopy={() => {}} copied={false} />],
    ['android', <AndroidPanel key="a" t={(k) => k} version="1.0.0" primaryLink="/a.apk" backupLink="/b.apk" />],
  ] as const)('%s backup link sends install_click with s=backup and stays a plain link', (platform, panel) => {
    render(panel);
    const backup = screen.getByText('install.install.backupDownload');
    expect(backup.getAttribute('href')).toMatch(/^\/b\./);
    fireEvent.click(backup);
    expect(clicks()).toEqual([`${platform}/backup`]);
    expect(openDownloadInNewTab).not.toHaveBeenCalled();
  });
});
