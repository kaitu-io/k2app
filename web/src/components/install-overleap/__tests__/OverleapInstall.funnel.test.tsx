/**
 * web_install funnel on this brand's download page: one install_view per mount,
 * install_click (plan = platform, s=button) for every real download or store
 * link, nothing for a "coming soon" placeholder.
 */
import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import OverleapInstall, { type InstallTarget } from '../OverleapInstall';

vi.mock('@/lib/device-detection', () => ({ detectDevice: () => ({ type: 'macos' }) }));
vi.mock('@/hooks/useBrand', () => ({ useBrand: () => ({ displayName: 'X' }) }));
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const TARGETS: InstallTarget[] = [
  { platform: 'windows', url: 'https://cdn.example/a.exe', version: '1.0.0' },
  { platform: 'macos', url: 'https://cdn.example/a.pkg', version: '1.0.0' },
  { platform: 'ios', url: 'https://apps.example/x', store: true },
  { platform: 'android', url: '', store: false },
];

describe('OverleapInstall funnel', () => {
  let srcs: string[];
  const events = () => srcs.map((s) => new URL(s, 'http://x').searchParams);
  const named = (e: string) => events().filter((p) => p.get('e') === e);
  const clicks = () => named('install_click').map((p) => `${p.get('p')}/${p.get('s')}`);

  beforeEach(() => {
    srcs = [];
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
  });
  afterEach(() => vi.unstubAllGlobals());

  const card = (p: string) => within(screen.getByTestId(`install-card-${p}`));

  it('install_view fires once per mount, not again on re-render, and no click is reported by itself', () => {
    const { rerender } = render(<OverleapInstall targets={TARGETS} />);
    rerender(<OverleapInstall targets={[...TARGETS]} />);
    expect(named('install_view')).toHaveLength(1);
    expect(clicks()).toEqual([]);
  });

  it.each(['windows', 'macos', 'ios'])('%s: the download / store link reports install_click with the platform, s=button', (platform) => {
    render(<OverleapInstall targets={TARGETS} />);
    const link = card(platform).getByRole('link');
    link.addEventListener('click', (e) => e.preventDefault());
    fireEvent.click(link);
    expect(clicks()).toEqual([`${platform}/button`]);
  });

  it('a coming-soon placeholder is not a link and reports nothing', () => {
    render(<OverleapInstall targets={TARGETS} />);
    expect(card('android').queryByRole('link')).toBeNull();
    fireEvent.click(card('android').getByRole('button'));
    expect(clicks()).toEqual([]);
  });
});
