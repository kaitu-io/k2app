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
const mockVPNMachine = {
  isServiceDown: false,
  error: null as { code: number; message: string } | null,
};

const mockVPNMachineStoreState = {
  initialization: null as any,
};

vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachine: () => mockVPNMachine,
  useVPNMachineStore: (selector: any) => selector(mockVPNMachineStoreState),
}));

// Mock vpn-types
vi.mock('../../services/vpn-types', () => ({
  isNetworkError: (code: number) => code === 408 || code === 503,
}));

describe('ServiceAlert', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockVPNMachine.isServiceDown = false;
    mockVPNMachine.error = null;
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
      mockVPNMachine.isServiceDown = true;
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.serviceFailure.title')).toBeInTheDocument();
    });

    it('should render when network error occurs', () => {
      mockVPNMachine.error = { code: 408, message: 'Connection timeout' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should prioritize service failure over network error', () => {
      mockVPNMachine.isServiceDown = true;
      mockVPNMachine.error = { code: 503, message: 'Server unreachable' };
      renderAlert();
      // Should show service failure message, not network error
      expect(screen.getByText('dashboard:dashboard.serviceFailure.title')).toBeInTheDocument();
    });
  });

  describe('Network Error Detection', () => {
    it('should detect error code 408 (timeout) as network error', () => {
      mockVPNMachine.error = { code: 408, message: 'Connection timeout' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should detect error code 503 (unreachable) as network error', () => {
      mockVPNMachine.error = { code: 503, message: 'Server unreachable' };
      renderAlert();
      expect(screen.getByText('dashboard:dashboard.networkError.title')).toBeInTheDocument();
    });

    it('should not show alert for non-network errors', () => {
      mockVPNMachine.error = { code: 570, message: 'Connection fatal' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });

    it('should not show alert for auth errors', () => {
      mockVPNMachine.error = { code: 401, message: 'Unauthorized' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });

    it('should not show alert for protocol errors', () => {
      mockVPNMachine.error = { code: 502, message: 'TLS handshake failed' };
      const { container } = renderAlert();
      expect(container.firstChild).toBeNull();
    });
  });

  describe('Navigation', () => {
    it('should navigate to service-error page on resolve click', () => {
      mockVPNMachine.isServiceDown = true;
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
      mockVPNMachine.isServiceDown = true;
      const { container } = renderAlert(250);

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      // Check inline style attribute contains the expected value
      expect(alertDiv.style.left).toBe('250px');
    });

    it('should have fixed positioning', () => {
      mockVPNMachine.isServiceDown = true;
      const { container } = renderAlert();

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      expect(alertDiv.style.position).toBe('fixed');
    });

    it('should have error styling (red background)', () => {
      mockVPNMachine.isServiceDown = true;
      const { container } = renderAlert();

      const alertDiv = container.firstChild as HTMLElement;
      expect(alertDiv).toBeInTheDocument();
      // Check that backgroundColor contains the expected value
      expect(alertDiv.style.backgroundColor).toContain('#FEF2F2');
    });
  });

  describe('Hover Effects', () => {
    it('should show underline on hover', () => {
      mockVPNMachine.isServiceDown = true;
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
