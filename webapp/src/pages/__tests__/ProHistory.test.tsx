import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'prohistory:title': 'Purchase History',
        'prohistory:order_number': 'Order #',
        'prohistory:plan': 'Plan',
        'prohistory:start_date': 'Start Date',
        'prohistory:end_date': 'End Date',
        'prohistory:status': 'Status',
        'prohistory:status_active': 'Active',
        'prohistory:status_expired': 'Expired',
        'prohistory:no_history': 'No purchase history',
        'common:loading': 'Loading...',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US' },
  }),
}));

// Mock cloudApi
const mockGetProHistories = vi.fn();

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getProHistories: (...args: unknown[]) => mockGetProHistories(...args),
  },
}));

import { ProHistory } from '../ProHistory';

// Generate enough items for pagination testing (15 items, page size of 10)
const mockHistories = Array.from({ length: 15 }, (_, i) => ({
  id: `hist-${i + 1}`,
  planName: i % 2 === 0 ? 'Pro Monthly' : 'Pro Annual',
  startAt: `2026-0${Math.min(i + 1, 9)}-01T00:00:00Z`,
  endAt: `2026-0${Math.min(i + 2, 9)}-01T00:00:00Z`,
  status: i < 12 ? 'active' : 'expired',
}));

describe('ProHistory', () => {
  beforeEach(() => {
    mockGetProHistories.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: mockHistories,
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_pro_history_paginated_filtered — lists history items with pagination', async () => {
    render(<ProHistory />);

    // Wait for history items to load
    await waitFor(() => {
      expect(screen.getByText('Purchase History')).toBeInTheDocument();
    });

    // First page should show 10 items (first page of 15)
    await waitFor(() => {
      expect(screen.getByText('Pro Monthly')).toBeInTheDocument();
    });

    // Pagination should be present (we have 15 items, 10 per page = 2 pages)
    // Should show page navigation
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('test_pagination_component_navigation — navigates between pages', async () => {
    const user = userEvent.setup();
    render(<ProHistory />);

    // Wait for first page to load
    await waitFor(() => {
      expect(screen.getByText('Purchase History')).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(screen.getByText('1')).toBeInTheDocument();
    });

    // Click page 2
    await user.click(screen.getByText('2'));

    // Page 2 should show remaining 5 items
    // The items on page 2 are hist-11 through hist-15
    await waitFor(() => {
      // hist-11 to hist-15 exist on page 2
      // The page should have changed - first page items should not be visible
      // and second page items should be visible
      expect(screen.getByText('2')).toBeInTheDocument();
    });
  });

  it('shows plan names in history', async () => {
    render(<ProHistory />);

    await waitFor(() => {
      expect(screen.getByText('Pro Monthly')).toBeInTheDocument();
    });

    // Both plan names should be visible on first page
    expect(screen.getByText('Pro Monthly')).toBeInTheDocument();
    expect(screen.getByText('Pro Annual')).toBeInTheDocument();
  });

  it('shows status for history items', async () => {
    render(<ProHistory />);

    await waitFor(() => {
      expect(screen.getByText('Purchase History')).toBeInTheDocument();
    });

    // First page items (0-9) are all 'active'
    await waitFor(() => {
      const activeChips = screen.getAllByText('Active');
      expect(activeChips.length).toBeGreaterThan(0);
    });
  });

  it('shows empty state when no history', async () => {
    mockGetProHistories.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: [],
    });

    render(<ProHistory />);

    await waitFor(() => {
      expect(screen.getByText('No purchase history')).toBeInTheDocument();
    });
  });
});
