import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'title': 'Help & Support',
        'faqTitle': 'Frequently Asked Questions',
        'faqConnectionTitle': 'Connection Issues',
        'faqConnectionDesc': 'Having trouble connecting? Check your network settings and try again.',
        'faqAccountTitle': 'Account & Billing',
        'faqAccountDesc': 'Manage your subscription, payment methods, and account settings.',
        'faqSpeedTitle': 'Speed Optimization',
        'faqSpeedDesc': 'Tips to improve your connection speed and performance.',
        'faqSecurityTitle': 'Security & Privacy',
        'faqSecurityDesc': 'Learn about our encryption and privacy protection features.',
        'viewIssues': 'View Issues',
        'submitTicket': 'Submit Ticket',
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

import { FAQ } from '../FAQ';

function renderFAQ() {
  return render(
    <MemoryRouter>
      <FAQ />
    </MemoryRouter>
  );
}

describe('FAQ', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_faq_help_cards', () => {
    renderFAQ();

    // Should display the title
    expect(screen.getByText('Help & Support')).toBeInTheDocument();

    // Should display all 4 FAQ cards with titles and descriptions
    expect(screen.getByText('Connection Issues')).toBeInTheDocument();
    expect(screen.getByText('Having trouble connecting? Check your network settings and try again.')).toBeInTheDocument();

    expect(screen.getByText('Account & Billing')).toBeInTheDocument();
    expect(screen.getByText('Manage your subscription, payment methods, and account settings.')).toBeInTheDocument();

    expect(screen.getByText('Speed Optimization')).toBeInTheDocument();
    expect(screen.getByText('Tips to improve your connection speed and performance.')).toBeInTheDocument();

    expect(screen.getByText('Security & Privacy')).toBeInTheDocument();
    expect(screen.getByText('Learn about our encryption and privacy protection features.')).toBeInTheDocument();
  });

  it('test_faq_links_to_issues_and_ticket', async () => {
    const user = userEvent.setup();
    renderFAQ();

    // Should have "View Issues" button that navigates to /issues
    const viewIssuesBtn = screen.getByText('View Issues');
    expect(viewIssuesBtn).toBeInTheDocument();
    await user.click(viewIssuesBtn);
    expect(mockNavigate).toHaveBeenCalledWith('/issues');

    vi.clearAllMocks();

    // Should have "Submit Ticket" button that navigates to /submit-ticket
    const submitTicketBtn = screen.getByText('Submit Ticket');
    expect(submitTicketBtn).toBeInTheDocument();
    await user.click(submitTicketBtn);
    expect(mockNavigate).toHaveBeenCalledWith('/submit-ticket');
  });
});
