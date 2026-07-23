/**
 * CountryFilterDialog — auto-pick country exclusion filter.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/CountryFilterDialog.test.tsx
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CountryFilterDialog } from '../CountryFilterDialog';
import type { Tunnel } from '../../services/api-types';

// Mock MUI Dialog to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom). Same pattern
// as BetaChannelToggle.test.tsx / LoginDialog.test.tsx.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => `name-${code}`,
  getFlagIcon: (code: string) => `flag-${code}`,
}));

const makeTunnel = (id: number, country: string) => ({
  id,
  domain: `t${id}.example.com`,
  name: `t${id}`,
  serverUrl: 'k2v5://x',
  node: { country },
  recommendScore: 0.5,
}) as unknown as Tunnel;

const tunnels = [makeTunnel(1, 'JP'), makeTunnel(2, 'JP'), makeTunnel(3, 'HK')];

describe('CountryFilterDialog', () => {
  const onToggle = vi.fn();
  const onClear = vi.fn();
  const onClose = vi.fn();

  beforeEach(() => {
    onToggle.mockReset();
    onClear.mockReset();
    onClose.mockReset();
  });

  const props = { open: true, onClose, tunnels, excludedCountries: [] as string[], onToggle, onClear };

  it('renders one row per country, count-desc', () => {
    render(<CountryFilterDialog {...props} />);
    const rows = screen.getAllByRole('button').filter(el => el.textContent?.includes('name-'));
    expect(rows[0]).toHaveTextContent('name-jp'); // 2 nodes first
    expect(rows[1]).toHaveTextContent('name-hk');
  });

  it('checkbox reflects exclusion state', () => {
    render(<CountryFilterDialog {...props} excludedCountries={['hk']} />);
    const checkboxes = screen.getAllByRole('checkbox');
    expect(checkboxes[0]).not.toBeChecked(); // jp
    expect(checkboxes[1]).toBeChecked();     // hk
  });

  it('clicking a row calls onToggle with the code', () => {
    render(<CountryFilterDialog {...props} />);
    fireEvent.click(screen.getByText('name-hk'));
    expect(onToggle).toHaveBeenCalledWith('hk');
  });

  it('clear button disabled when nothing excluded, calls onClear otherwise', () => {
    const { rerender } = render(<CountryFilterDialog {...props} />);
    expect(screen.getByTestId('country-filter-clear')).toBeDisabled();
    rerender(<CountryFilterDialog {...props} excludedCountries={['hk']} />);
    fireEvent.click(screen.getByTestId('country-filter-clear'));
    expect(onClear).toHaveBeenCalled();
  });

  it('done button calls onClose', () => {
    render(<CountryFilterDialog {...props} />);
    fireEvent.click(screen.getByTestId('country-filter-done'));
    expect(onClose).toHaveBeenCalled();
  });
});
