/**
 * ConnectionButton — no-membership upsell
 *
 * A new device account has no membership, so the cloud list is empty and no
 * tunnel can be selected. Without `upsellHint` the main button is disabled
 * and says "select a server" — the largest element on the home screen does
 * nothing when tapped. With `upsellHint` it stays tappable (the caller routes
 * the tap to /purchase) and shows the hint instead.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/ConnectionButton.upsell.test.tsx
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ConnectionButton } from '../ConnectionButton';
import { CompactConnectionButton } from '../CompactConnectionButton';

const base = {
  serviceState: 'disconnected' as const,
  hasTunnelSelected: false,
};

describe('ConnectionButton upsellHint', () => {
  it('without upsellHint, no tunnel → disabled with the select-server hint', () => {
    const onToggle = vi.fn();
    render(<ConnectionButton {...base} onToggle={onToggle} />);
    expect(screen.getByRole('button')).toBeDisabled();
    expect(screen.getByText(/请选择服务器|select a server/i)).toBeInTheDocument();
  });

  it('with upsellHint, no tunnel → tappable, shows the hint, tap reaches onToggle', () => {
    const onToggle = vi.fn();
    render(<ConnectionButton {...base} onToggle={onToggle} upsellHint="点击开通会员" />);
    const btn = screen.getByRole('button');
    expect(btn).not.toBeDisabled();
    expect(screen.getByText('点击开通会员')).toBeInTheDocument();
    expect(screen.queryByText(/请选择服务器/)).not.toBeInTheDocument();
    fireEvent.click(btn);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('dormant still wins over upsellHint (logged-out: the CTA is login, not purchase)', () => {
    render(<ConnectionButton {...base} onToggle={vi.fn()} upsellHint="点击开通会员" dormant />);
    expect(screen.getByRole('button')).toBeDisabled();
  });
});

describe('CompactConnectionButton upsellHint', () => {
  it('collapsed layout gets the same treatment: switch enabled, hint replaces select-server', () => {
    const onToggle = vi.fn();
    render(<CompactConnectionButton {...base} onToggle={onToggle} upsellHint="点击开通会员" />);
    expect(screen.getByText('点击开通会员')).toBeInTheDocument();
    const sw = screen.getByRole('checkbox');
    expect(sw).not.toBeDisabled();
    fireEvent.click(sw);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
