/**
 * ServiceAlert Component Tests
 *
 * Tests service failure and network error alert display
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ServiceAlert from '../ServiceAlert';

// Mock react-i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback || key,
    i18n: { language: 'en' },
  }),
}));

// Mock react-router-dom navigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useLocation: () => ({ pathname: '/dashboard' }),
  };
});

// Mock stores
const mockVPNStatus = {
  isServiceFailedLongTime: false,
  error: null as { code: number; message: string } | null,
};

const mockVPNStoreState = {
  status: {
    initialization: null as any,
  },
};

vi.mock('../../stores', () => ({
  useVPNStatus: () => mockVPNStatus,
  useVPNStore: (selector: any) => selector(mockVPNStoreState),
}));

// Mock control-types
vi.mock('../../services/control-types', () => ({
  isNetworkError: (code: number) => code >= 100 && code <= 109,
}));

describe('ServiceAlert', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockVPNStatus.isServiceFailedLongTime = false;
    mockVPNStatus.error = null;
  });

  const renderAlert = (sidebarWidth = 0) => {
    return render(
      <MemoryRouter>
        <ServiceAlert sidebarWidth={sidebarWidth} />
      </MemoryRouter>
    );
  };

  describe('Visibility', () => {
    it('should not render when no errors', () => {
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });

    it('should render when service failed for long time', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.serviceFailure.title')).toBeInTheDocument();
    });

    it('should render when network error occurs', () => {
      mockVPNStatus.error = { code: 100, message: 'Network error' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should prioritize service failure over network error', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      mockVPNStatus.error = { code: 100, message: 'Network error' };
      renderAlert();
      // Should show service failure message, not network error
      expect(screen.getByText('dashboard:dashboard.serviceFailure.title')).toBeInTheDocument();
    });
  });

  describe('Network Error Detection', () => {
    it('should detect error code 100 as network error', () => {
      mockVPNStatus.error = { code: 100, message: 'Error 100' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should detect error code 109 as network error', () => {
      mockVPNStatus.error = { code: 109, message: 'Error 109' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should not show alert for non-network errors', () => {
      mockVPNStatus.error = { code: 500, message: 'Server error' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });

    it('should not show alert for error code 99', () => {
      mockVPNStatus.error = { code: 99, message: 'Error 99' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });

    it('should not show alert for error code 110', () => {
      mockVPNStatus.error = { code: 110, message: 'Error 110' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });
  });

  describe('Navigation', () => {
    it('should navigate to service-error page on resolve click', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      renderAlert();

      const resolveButton = screen.getByText('Resolve');
      fireEvent.click(resolveButton);

      expect(mockNavigate).toHaveBeenCalledWith('/service-error', {
        state: { from: '/dashboard' },
      });
    });
  });

  describe('Styling', () => {
    it('should respect sidebarWidth prop', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      const { container } = renderAlert(250);

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      // Check inline style attribute contains the expected value
      expect(alertDiv.style.left).toBe('250px');
    });

    it('should have fixed positioning', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      const { container } = renderAlert();

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      expect(alertDiv.style.position).toBe('fixed');
    });

    it('should have error styling (red background)', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      const { container } = renderAlert();

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      // Check that backgroundColor contains the expected value
      expect(alertDiv.style.backgroundColor).toContain('#FEF2F2');
    });
  });

  describe('Hover Effects', () => {
    it('should show underline on hover', () => {
      mockVPNStatus.isServiceFailedLongTime = true;
      renderAlert();

      // Get the "More" button which has hover underline effect
      const moreButton = screen.getByText('More');
      expect(moreButton).toBeInTheDocument();

      // Trigger hover - the component sets inline style on hover
      fireEvent.mouseEnter(moreButton);
      expect(moreButton.style.textDecoration).toBe('underline');

      fireEvent.mouseLeave(moreButton);
      expect(moreButton.style.textDecoration).toBe('none');
    });
  });
});
