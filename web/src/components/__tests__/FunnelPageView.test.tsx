import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';

let pathname = '/a';
vi.mock('next/navigation', () => ({ usePathname: () => pathname }));
vi.mock('@/i18n/routing', () => ({ usePathname: () => pathname }));

describe('FunnelPageView', () => {
  let made: { src: string }[];
  beforeEach(() => {
    vi.resetModules();
    pathname = '/a';
    made = [];
    vi.stubGlobal('Image', function (this: { src: string }) { made.push(this); } as unknown as typeof Image);
    vi.spyOn(document, 'referrer', 'get').mockReturnValue('https://www.google.com/');
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('fires page_view on mount, again on path change, not on identical rerender; ref only on the first', async () => {
    const { default: FunnelPageView } = await import('../FunnelPageView');
    const { rerender, container } = render(<FunnelPageView />);
    expect(container.innerHTML).toBe('');
    expect(made).toHaveLength(1);
    expect(made[0].src).toContain('e=page_view');
    expect(made[0].src).toContain('r=www.google.com');

    rerender(<FunnelPageView />);
    expect(made).toHaveLength(1);

    pathname = '/b';
    rerender(<FunnelPageView />);
    expect(made).toHaveLength(2);
    expect(made[1].src).toContain('e=page_view');
    expect(made[1].src).not.toContain('r=');

    rerender(<FunnelPageView />);
    expect(made).toHaveLength(2);
  });
});
