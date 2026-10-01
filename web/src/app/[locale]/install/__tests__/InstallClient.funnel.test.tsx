/**
 * web_install funnel on the install page: the automatic download after the
 * countdown IS the main desktop conversion, so it is counted (once per mount,
 * s=auto); a cancelled countdown is not. The CLI copy action counts as s=cli
 * for the tab it was copied from.
 */
import { act, fireEvent, render } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const triggerAutoDownload = vi.fn();
const detected = vi.hoisted(() => ({ type: 'windows' }));

vi.mock('@/lib/device-detection', () => ({
  detectDevice: () => ({ type: detected.type }),
  triggerAutoDownload: (...a: unknown[]) => triggerAutoDownload(...a),
  openDownloadInNewTab: vi.fn(),
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));
vi.mock('@/components/ui/tabs', () => ({
  Tabs: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsContent: ({ children, value }: { children: React.ReactNode; value: string }) => <div data-tab={value}>{children}</div>,
}));
vi.mock('@/components/ui/accordion', () => ({
  Accordion: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AccordionItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AccordionTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AccordionContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import InstallClient from '../InstallClient';

const MOBILE = {
  ios: { url: 'https://apps.example/x', version: '1.0.0' },
  android: { primary: 'https://cdn.example/a.apk', backup: 'https://cdn2.example/a.apk', version: '1.0.0' },
};

describe('InstallClient funnel', () => {
  let srcs: string[];
  const events = () => srcs.map((s) => new URL(s, 'http://x').searchParams);
  const clicks = () => events().filter((p) => p.get('e') === 'install_click').map((p) => `${p.get('p')}/${p.get('s')}`);

  beforeEach(() => {
    vi.useFakeTimers();
    srcs = [];
    detected.type = 'windows';
    window.history.pushState({}, '', '/zh-CN/install');
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn(() => Promise.resolve()) } });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  const mount = () => render(<InstallClient betaVersion={null} stableVersion="1.0.0" mobileLinks={MOBILE} />);
  const tick = (ms: number) => act(() => { vi.advanceTimersByTime(ms); });

  it.each(['windows', 'macos', 'android'])('%s: the automatic download counts once as install_click s=auto', (type) => {
    detected.type = type;
    mount();
    expect(clicks()).toEqual([]);
    tick(4000);
    expect(triggerAutoDownload).not.toHaveBeenCalled();
    expect(clicks()).toEqual([]);
    tick(1000);
    expect(triggerAutoDownload).toHaveBeenCalledTimes(1);
    expect(clicks()).toEqual([`${type}/auto`]);
    tick(60000);
    expect(clicks()).toEqual([`${type}/auto`]);
  });

  it('a cancelled countdown downloads nothing and counts nothing', () => {
    const { getByText } = mount();
    tick(2000);
    fireEvent.click(getByText('install.install.cancelAutoDownload'));
    tick(10000);
    expect(triggerAutoDownload).not.toHaveBeenCalled();
    expect(clicks()).toEqual([]);
  });

  it.each(['ios', 'linux', 'unknown'])('%s: no automatic download, no auto event', (type) => {
    detected.type = type;
    mount();
    tick(10000);
    expect(triggerAutoDownload).not.toHaveBeenCalled();
    expect(clicks()).toEqual([]);
  });

  it('?nodownload suppresses both the download and the event', () => {
    window.history.pushState({}, '', '/zh-CN/install?nodownload');
    mount();
    tick(10000);
    expect(triggerAutoDownload).not.toHaveBeenCalled();
    expect(clicks()).toEqual([]);
  });

  it('copying the CLI command on the Linux tab counts as install_click p=linux s=cli', () => {
    detected.type = 'linux';
    const { container } = mount();
    const copy = container.querySelector('[data-tab="linux"] code + button') as HTMLElement;
    expect(copy).not.toBeNull();
    fireEvent.click(copy);
    expect(clicks()).toEqual(['linux/cli']);
    expect(events().filter((p) => p.get('e') === 'install_view')).toHaveLength(1);
  });
});
