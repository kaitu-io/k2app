import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'submitTicketTitle': 'Submit Ticket',
        'subject': 'Subject',
        'subjectPlaceholder': 'Brief description of the issue',
        'content': 'Content',
        'contentPlaceholder': 'Describe the issue in detail...',
        'uploadLogs': 'Upload Logs',
        'submit': 'Submit',
        'submitting': 'Submitting...',
        'submitSuccess': 'Ticket submitted successfully',
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
const mockCreateIssue = vi.fn();
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    createIssue: (...args: unknown[]) => mockCreateIssue(...args),
  },
}));

// Mock platform
const mockUploadLogs = vi.fn();
vi.mock('../../platform', () => ({
  getPlatform: () => ({
    uploadLogs: mockUploadLogs,
  }),
}));

import { SubmitTicket } from '../SubmitTicket';

function renderSubmitTicket() {
  return render(
    <MemoryRouter>
      <SubmitTicket />
    </MemoryRouter>
  );
}

describe('SubmitTicket', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateIssue.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: { id: 'new-issue-1' },
    });
    mockUploadLogs.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
  });

  it('test_submit_ticket_sends', async () => {
    const user = userEvent.setup();
    renderSubmitTicket();

    // Should show title
    expect(screen.getByText('Submit Ticket')).toBeInTheDocument();

    // Should show subject and content fields
    const subjectInput = screen.getByPlaceholderText('Brief description of the issue');
    const contentInput = screen.getByPlaceholderText('Describe the issue in detail...');
    expect(subjectInput).toBeInTheDocument();
    expect(contentInput).toBeInTheDocument();

    // Fill in the form
    await user.type(subjectInput, 'App crashes on startup');
    await user.type(contentInput, 'When I open the app, it crashes immediately after the splash screen.');

    // Submit the form
    const submitBtn = screen.getByText('Submit');
    await user.click(submitBtn);

    // Should call createIssue with subject and content
    expect(mockCreateIssue).toHaveBeenCalledWith(
      'App crashes on startup',
      'When I open the app, it crashes immediately after the splash screen.'
    );

    // After success, should navigate to /issues
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/issues');
    });
  });

  it('test_submit_ticket_uploads_logs', async () => {
    const user = userEvent.setup();
    renderSubmitTicket();

    // Fill in required fields
    const subjectInput = screen.getByPlaceholderText('Brief description of the issue');
    const contentInput = screen.getByPlaceholderText('Describe the issue in detail...');
    await user.type(subjectInput, 'Connection issue with logs');
    await user.type(contentInput, 'Please see attached logs.');

    // Click Upload Logs button
    const uploadLogsBtn = screen.getByText('Upload Logs');
    await user.click(uploadLogsBtn);

    // Submit the form
    const submitBtn = screen.getByText('Submit');
    await user.click(submitBtn);

    // Should call createIssue
    expect(mockCreateIssue).toHaveBeenCalledWith(
      'Connection issue with logs',
      'Please see attached logs.'
    );

    // After issue is created, uploadLogs should be called with the new issue ID
    await waitFor(() => {
      expect(mockUploadLogs).toHaveBeenCalledWith('new-issue-1');
    });
  });
});
