import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        expiration: 'Expiration',
        'expirationOptions.1h': '1 Hour',
        'expirationOptions.24h': '24 Hours',
        'expirationOptions.7d': '7 Days',
        'expirationOptions.30d': '30 Days',
        'expirationOptions.never': 'Never',
      };
      return map[key] || key;
    },
  }),
}));

import { ExpirationSelectorPopover } from '../ExpirationSelectorPopover';

describe('ExpirationSelectorPopover', () => {
  const mockOnSelect = vi.fn();
  const mockOnClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('renders all expiration options when open', () => {
    render(
      <ExpirationSelectorPopover
        open={true}
        onSelect={mockOnSelect}
        onClose={mockOnClose}
      />
    );
    expect(screen.getByText('Expiration')).toBeInTheDocument();
    expect(screen.getByText('1 Hour')).toBeInTheDocument();
    expect(screen.getByText('24 Hours')).toBeInTheDocument();
    expect(screen.getByText('7 Days')).toBeInTheDocument();
    expect(screen.getByText('30 Days')).toBeInTheDocument();
    expect(screen.getByText('Never')).toBeInTheDocument();
  });

  it('does not render content when closed', () => {
    render(
      <ExpirationSelectorPopover
        open={false}
        onSelect={mockOnSelect}
        onClose={mockOnClose}
      />
    );
    expect(screen.queryByText('Expiration')).not.toBeInTheDocument();
  });

  it('calls onSelect with the chosen expiration value', async () => {
    render(
      <ExpirationSelectorPopover
        open={true}
        onSelect={mockOnSelect}
        onClose={mockOnClose}
      />
    );
    await userEvent.click(screen.getByText('24 Hours'));
    expect(mockOnSelect).toHaveBeenCalledWith('24h');
  });

  it('calls onClose after selection', async () => {
    render(
      <ExpirationSelectorPopover
        open={true}
        onSelect={mockOnSelect}
        onClose={mockOnClose}
      />
    );
    await userEvent.click(screen.getByText('7 Days'));
    expect(mockOnClose).toHaveBeenCalled();
  });

  it('highlights the currently selected option', () => {
    render(
      <ExpirationSelectorPopover
        open={true}
        selected="7d"
        onSelect={mockOnSelect}
        onClose={mockOnClose}
      />
    );
    const selectedOption = screen.getByTestId('expiration-option-7d');
    expect(selectedOption.className).toMatch(/bg-/);
  });
});
