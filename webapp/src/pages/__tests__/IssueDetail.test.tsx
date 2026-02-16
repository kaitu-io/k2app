import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'issueDetail': 'Issue Detail',
        'comments': 'Comments',
        'addComment': 'Add Comment',
        'commentPlaceholder': 'Write a comment...',
        'submit': 'Submit',
        'noComments': 'No comments yet',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US' },
  }),
}));

// Mock cloud API
const mockGetIssueDetail = vi.fn();
const mockGetIssueComments = vi.fn();
const mockAddComment = vi.fn();
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getIssueDetail: (...args: unknown[]) => mockGetIssueDetail(...args),
    getIssueComments: (...args: unknown[]) => mockGetIssueComments(...args),
    addComment: (...args: unknown[]) => mockAddComment(...args),
  },
}));

import { IssueDetail } from '../IssueDetail';

function renderIssueDetail(issueId = 'issue-1') {
  return render(
    <MemoryRouter initialEntries={[`/issues/${issueId}`]}>
      <Routes>
        <Route path="/issues/:id" element={<IssueDetail />} />
      </Routes>
    </MemoryRouter>
  );
}

const mockIssue = {
  id: 'issue-1',
  title: 'Cannot connect to server',
  content: 'I am unable to connect to any server. I have tried restarting the app.',
  status: 'open',
  createdAt: '2025-12-01T10:00:00Z',
  updatedAt: '2025-12-02T10:00:00Z',
};

const mockComments = [
  {
    id: 'comment-1',
    issueId: 'issue-1',
    content: 'Please try clearing your cache.',
    author: 'support@kaitu.io',
    createdAt: '2025-12-01T12:00:00Z',
  },
  {
    id: 'comment-2',
    issueId: 'issue-1',
    content: 'I tried that but still having the issue.',
    author: 'user@example.com',
    createdAt: '2025-12-01T14:00:00Z',
  },
];

describe('IssueDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetIssueDetail.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: mockIssue,
    });
    mockGetIssueComments.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: mockComments,
    });
    mockAddComment.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: null,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it('test_issue_detail_comments_reply', async () => {
    const user = userEvent.setup();
    renderIssueDetail();

    // Wait for issue detail to load
    await waitFor(() => {
      expect(screen.getByText('Cannot connect to server')).toBeInTheDocument();
    });

    // Should show issue content
    expect(
      screen.getByText('I am unable to connect to any server. I have tried restarting the app.')
    ).toBeInTheDocument();

    // Should show comments section
    expect(screen.getByText('Comments')).toBeInTheDocument();

    // Should show both comments with authors and content
    expect(screen.getByText('Please try clearing your cache.')).toBeInTheDocument();
    expect(screen.getByText('support@kaitu.io')).toBeInTheDocument();

    expect(screen.getByText('I tried that but still having the issue.')).toBeInTheDocument();
    expect(screen.getByText('user@example.com')).toBeInTheDocument();

    // Should be able to add a comment
    const textarea = screen.getByPlaceholderText('Write a comment...');
    expect(textarea).toBeInTheDocument();

    await user.type(textarea, 'This is my reply');

    const submitBtn = screen.getByText('Submit');
    await user.click(submitBtn);

    // Should call addComment with issueId and content
    expect(mockAddComment).toHaveBeenCalledWith('issue-1', 'This is my reply');

    // After submitting, comments should be refetched
    await waitFor(() => {
      // getIssueComments called initially and after adding comment
      expect(mockGetIssueComments).toHaveBeenCalledTimes(2);
    });
  });
});
