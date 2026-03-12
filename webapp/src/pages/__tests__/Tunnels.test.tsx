/**
 * Tunnels 页面测试
 *
 * 测试自部署节点管理页面的核心功能:
 * - Deploy command with copy
 * - URI input + save + validation
 * - Cloud hint for guests vs authenticated
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

// ==================== Helpers ====================

/** Get the URI input (second textbox — first is deploy command) */
function getUriInput(): HTMLInputElement {
  const inputs = screen.getAllByRole('textbox');
  return inputs[1] as HTMLInputElement;
}

/** Get the deploy command input (first textbox) */
function getDeployInput(): HTMLInputElement {
  const inputs = screen.getAllByRole('textbox');
  return inputs[0] as HTMLInputElement;
}

// ==================== Tests ====================

describe('Tunnels', () => {
  describe('deploy command', () => {
    it('renders deploy command in read-only input', () => {
      setupMocks();
      render(<Tunnels />);

      const input = getDeployInput();
      expect(input.value).toContain('curl -fsSL');
      expect(input.value).toContain('sudo sh');
      expect(input).toHaveAttribute('readonly');
    });

    it('copies command via _platform.writeClipboard', async () => {
      setupMocks();
      render(<Tunnels />);

      // Copy button has aria-label from Tooltip
      const copyButton = screen.getByLabelText(/^复制$|^Copy$/i);
      fireEvent.click(copyButton);

      await waitFor(() => {
        expect((window as any)._platform.writeClipboard).toHaveBeenCalledWith(
          'curl -fsSL https://kaitu.io/i/k2s | sudo sh'
        );
      });
    });
  });

  describe('URI input', () => {
    it('renders empty input when no tunnel saved', () => {
      setupMocks();
      render(<Tunnels />);

      const input = getUriInput();
      expect(input).toBeInTheDocument();
      expect(input.value).toBe('');
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

      const input = getUriInput();
      expect(input.value).toContain('long***');
      expect(input.value).not.toContain('longtoken123');
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

      const input = getUriInput();
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

      const input = getUriInput();
      fireEvent.change(input, { target: { value: 'https://not-k2v5' } });

      const saveButton = screen.getByText(/^save$|^保存$/i).closest('button')!;
      fireEvent.click(saveButton);

      // saveTunnel should not be called — validation catches it
      await waitFor(() => {
        expect(mockSaveTunnel).not.toHaveBeenCalled();
      });
    });

    it('calls clearTunnel when clicking clear button', async () => {
      setupMocks({
        tunnel: {
          uri: 'k2v5://user:pass@host:443',
          name: 'host',
        },
      });
      mockClearTunnel.mockResolvedValue(undefined);
      render(<Tunnels />);

      // Clear button (清空) is rendered when tunnel exists
      const clearButton = screen.getByText(/^clear$|^清空$/i).closest('button')!;
      fireEvent.click(clearButton);

      await waitFor(() => {
        expect(mockClearTunnel).toHaveBeenCalled();
      });
    });
  });

  describe('cloud hint', () => {
    it('shows upgrade hint for guests', () => {
      setupMocks({ isAuthenticated: false });
      render(<Tunnels />);

      // Guest should see upgrade text
      expect(screen.getByText(/cloud|云节点|雲節點|クラウド/i)).toBeInTheDocument();
    });

    it('opens login dialog when CTA clicked', () => {
      setupMocks({ isAuthenticated: false });
      render(<Tunnels />);

      const ctaButton = screen.getByText(/cloud|云节点|雲節點|クラウド/i).closest('button')!;
      fireEvent.click(ctaButton);

      expect(mockOpenLoginDialog).toHaveBeenCalledWith(
        expect.objectContaining({ trigger: 'tunnels-page' })
      );
    });

    it('does not show upgrade hint for authenticated users', () => {
      setupMocks({ isAuthenticated: true });
      render(<Tunnels />);

      expect(screen.queryByText(/cloud|云节点|雲節點|クラウド/i)).not.toBeInTheDocument();
    });
  });
});
