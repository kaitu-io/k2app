/**
 * Tunnels 页面测试
 *
 * 测试自部署节点管理页面的核心功能:
 * - URI input + save + validation
 * - Deploy guide terminal with copy
 * - Cloud CTA for guests vs authenticated
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock stores
vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return {
    ...actual,
    useAuthStore: vi.fn(),
  };
});

vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return {
    ...actual,
    useLoginDialogStore: vi.fn(),
  };
});

const mockSaveTunnel = vi.fn();
const mockClearTunnel = vi.fn();

vi.mock('../../stores/self-hosted.store', async () => {
  const actual = await vi.importActual('../../stores/self-hosted.store');
  return {
    ...actual,
    useSelfHostedStore: vi.fn(),
  };
});

import { useAuthStore } from '../../stores';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { useSelfHostedStore } from '../../stores/self-hosted.store';
import Tunnels from '../Tunnels';

// ==================== Setup ====================

const mockOpenLoginDialog = vi.fn();

function setupMocks(overrides?: {
  isAuthenticated?: boolean;
  tunnel?: { uri: string; name: string; country?: string } | null;
}) {
  const { isAuthenticated = false, tunnel = null } = overrides ?? {};

  (useAuthStore as any).mockImplementation((selector: any) =>
    selector({ isAuthenticated })
  );

  (useLoginDialogStore as any).mockImplementation((selector: any) =>
    selector({ open: mockOpenLoginDialog })
  );

  (useSelfHostedStore as any).mockImplementation((selector: any) =>
    selector({
      tunnel,
      saveTunnel: mockSaveTunnel,
      clearTunnel: mockClearTunnel,
    })
  );
}

beforeEach(() => {
  (window as any)._platform = {
    os: 'macos' as const,
    version: '0.4.0',
    storage: { get: vi.fn(), set: vi.fn(), remove: vi.fn() },
    openExternal: vi.fn(),
    writeClipboard: vi.fn(),
  };

  mockSaveTunnel.mockReset();
  mockClearTunnel.mockReset();
  mockOpenLoginDialog.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as any)._platform;
});

// ==================== Tests ====================

describe('Tunnels', () => {
  describe('URI input', () => {
    it('renders empty input when no tunnel saved', () => {
      setupMocks();
      render(<Tunnels />);

      const input = screen.getByRole('textbox');
      expect(input).toBeInTheDocument();
      expect((input as HTMLInputElement).value).toBe('');
    });

    it('renders masked URI when tunnel exists', () => {
      setupMocks({
        tunnel: {
          uri: 'k2v5://alice:longtoken123@1.2.3.4:443#tokyo',
          name: 'tokyo',
          country: 'JP',
        },
      });
      render(<Tunnels />);

      const input = screen.getByRole('textbox');
      expect((input as HTMLInputElement).value).toContain('long***');
      expect((input as HTMLInputElement).value).not.toContain('longtoken123');
    });

    it('shows country and name when tunnel exists', () => {
      setupMocks({
        tunnel: {
          uri: 'k2v5://alice:token@1.2.3.4:443#tokyo',
          name: 'tokyo',
          country: 'JP',
        },
      });
      render(<Tunnels />);

      expect(screen.getByText(/JP/)).toBeInTheDocument();
      expect(screen.getByText(/tokyo/)).toBeInTheDocument();
    });

    it('calls saveTunnel on save with valid URI', async () => {
      setupMocks();
      mockSaveTunnel.mockResolvedValue(undefined);
      render(<Tunnels />);

      const input = screen.getByRole('textbox');
      fireEvent.change(input, { target: { value: 'k2v5://user:pass@host:443' } });

      // Find save button by text content
      const saveButton = screen.getByText(/^save$|^保存$/i).closest('button')!;
      fireEvent.click(saveButton);

      await waitFor(() => {
        expect(mockSaveTunnel).toHaveBeenCalledWith('k2v5://user:pass@host:443');
      });
    });

    it('does not call saveTunnel for invalid URI', async () => {
      setupMocks();
      render(<Tunnels />);

      const input = screen.getByRole('textbox');
      fireEvent.change(input, { target: { value: 'https://not-k2v5' } });

      const saveButton = screen.getByText(/^save$|^保存$/i).closest('button')!;
      fireEvent.click(saveButton);

      // saveTunnel should not be called — validation catches it
      await waitFor(() => {
        expect(mockSaveTunnel).not.toHaveBeenCalled();
      });
    });

    it('calls clearTunnel when saving empty input', async () => {
      setupMocks({
        tunnel: {
          uri: 'k2v5://user:pass@host:443',
          name: 'host',
        },
      });
      mockClearTunnel.mockResolvedValue(undefined);
      render(<Tunnels />);

      const input = screen.getByRole('textbox');
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: '' } });

      const saveButton = screen.getByText(/^save$|^保存$/i).closest('button')!;
      fireEvent.click(saveButton);

      await waitFor(() => {
        expect(mockClearTunnel).toHaveBeenCalled();
      });
    });
  });

  describe('deploy guide', () => {
    it('renders terminal block with curl command', () => {
      setupMocks();
      render(<Tunnels />);

      expect(screen.getByText(/curl -fsSL/)).toBeInTheDocument();
      expect(screen.getByText(/k2s setup/)).toBeInTheDocument();
    });
  });

  describe('cloud CTA', () => {
    it('shows upgrade CTA for guests', () => {
      setupMocks({ isAuthenticated: false });
      render(<Tunnels />);

      // Guest should see upgrade text
      expect(screen.getByText(/free|试用|体験/i)).toBeInTheDocument();
    });

    it('opens login dialog when CTA clicked', () => {
      setupMocks({ isAuthenticated: false });
      render(<Tunnels />);

      const ctaButton = screen.getByText(/free|试用|体験/i).closest('button')!;
      fireEvent.click(ctaButton);

      expect(mockOpenLoginDialog).toHaveBeenCalledWith(
        expect.objectContaining({ trigger: 'tunnels-page' })
      );
    });

    it('does not show upgrade CTA for authenticated users', () => {
      setupMocks({ isAuthenticated: true });
      render(<Tunnels />);

      expect(screen.queryByText(/free|试用|体験/i)).not.toBeInTheDocument();
    });
  });
});
