import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'title': 'Issues',
        'open': 'Open',
        'closed': 'Closed',
        'loadMore': 'Load More',
        'noIssues': 'No issues found',
        'comments': 'comments',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US' },
  }),
}));

// Mock navigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Mock cloud API
const mockGetIssues = vi.fn();
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getIssues: (...args: unknown[]) => mockGetIssues(...args),
  },
}));

import { Issues } from '../Issues';

function renderIssues() {
  return render(
    <MemoryRouter>
      <Issues />
    </MemoryRouter>
  );
}

const mockIssuesPage1 = [
  {
    id: 'issue-1',
    title: 'Cannot connect to server',
    content: 'Details here',
    status: 'open',
    commentCount: 3,
    createdAt: '2025-12-01T10:00:00Z',
    updatedAt: '2025-12-02T10:00:00Z',
  },
  {
    id: 'issue-2',
    title: 'Slow connection speed',
    content: 'Speed issue',
    status: 'closed',
    commentCount: 1,
    createdAt: '2025-11-15T08:00:00Z',
    updatedAt: '2025-11-20T08:00:00Z',
  },
];

const mockIssuesPage2 = [
  {
    id: 'issue-3',
    title: 'App crashes on startup',
    content: 'Crash report',
    status: 'open',
    commentCount: 0,
    createdAt: '2025-10-01T05:00:00Z',
    updatedAt: '2025-10-05T05:00:00Z',
  },
];

describe('Issues', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('test_issues_list_status_pagination', async () => {
    // First call returns page 1 with hasMore=true
    mockGetIssues
      .mockResolvedValueOnce({
        code: 0,
        message: 'ok',
        data: { issues: mockIssuesPage1, hasMore: true },
      })
      // Second call returns page 2 with hasMore=false
      .mockResolvedValueOnce({
        code: 0,
        message: 'ok',
        data: { issues: mockIssuesPage2, hasMore: false },
      });

    const user = userEvent.setup();
    renderIssues();

    // Wait for issues to load
    await waitFor(() => {
      expect(screen.getByText('Cannot connect to server')).toBeInTheDocument();
    });

    // Should show title
    expect(screen.getByText('Issues')).toBeInTheDocument();

    // Should show both issues
    expect(screen.getByText('Cannot connect to server')).toBeInTheDocument();
    expect(screen.getByText('Slow connection speed')).toBeInTheDocument();

    // Should show status chips
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText('Closed')).toBeInTheDocument();

    // Should show Load More button (hasMore=true)
    const loadMoreBtn = screen.getByText('Load More');
    expect(loadMoreBtn).toBeInTheDocument();

    // Click Load More to load page 2
    await user.click(loadMoreBtn);

    // Wait for page 2 data
    await waitFor(() => {
      expect(screen.getByText('App crashes on startup')).toBeInTheDocument();
    });

    // All 3 issues should now be visible
    expect(screen.getByText('Cannot connect to server')).toBeInTheDocument();
    expect(screen.getByText('Slow connection speed')).toBeInTheDocument();
    expect(screen.getByText('App crashes on startup')).toBeInTheDocument();

    // Load More should be gone (hasMore=false)
    expect(screen.queryByText('Load More')).not.toBeInTheDocument();

    // Click on an issue to navigate to detail
    await user.click(screen.getByText('Cannot connect to server'));
    expect(mockNavigate).toHaveBeenCalledWith('/issues/issue-1');
  });
});
