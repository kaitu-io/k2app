import { describe, it, expect } from 'vitest';
import { tunnelListSx, TUNNEL_LIST_MAX_WIDTH } from '../dashboard-layout';

describe('tunnelListSx', () => {
  it('desktop layout caps the tunnel list width and centres it', () => {
    const sx = tunnelListSx(true) as Record<string, unknown>;
    expect(sx.maxWidth).toBe(TUNNEL_LIST_MAX_WIDTH);
    expect(sx.width).toBe('100%');
    expect(sx.mx).toBe('auto');
  });

  it('mobile layout leaves the list full-bleed (phone window unchanged)', () => {
    const sx = tunnelListSx(false) as Record<string, unknown>;
    expect(sx.maxWidth).toBeUndefined();
    expect(sx.mx).toBeUndefined();
  });

  it('cap is wide enough for the sidebar layout but narrower than a 1040px window', () => {
    expect(TUNNEL_LIST_MAX_WIDTH).toBeGreaterThanOrEqual(640);
    expect(TUNNEL_LIST_MAX_WIDTH).toBeLessThan(1040 - 220);
  });
});
