import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';
import FunnelEvent from '../FunnelEvent';

describe('FunnelEvent', () => {
  let srcs: string[];
  beforeEach(() => {
    srcs = [];
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('fires its event once on mount, renders nothing, and stays quiet on re-render', () => {
    const { container, rerender } = render(<FunnelEvent event="pricing_view" />);
    expect(container.innerHTML).toBe('');
    expect(srcs).toHaveLength(1);
    expect(new URL(srcs[0], 'http://x').searchParams.get('e')).toBe('pricing_view');
    rerender(<FunnelEvent event="pricing_view" />);
    expect(srcs).toHaveLength(1);
  });
});
