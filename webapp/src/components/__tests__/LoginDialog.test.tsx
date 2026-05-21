import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom). Mirrors the
// pattern used in BetaChannelToggle.test.tsx.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogContentText: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

import LoginDialog from '../LoginDialog';
import { useLoginDialogStore } from '../../stores/login-dialog.store';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    post: vi.fn().mockResolvedValue({
      code: 0,
      data: { accessToken: 'x', refreshToken: 'y' },
    }),
  },
}));

vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid'),
}));

vi.mock('../../services/cache-store', () => ({
  cacheStore: { clear: vi.fn() },
}));

vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({ isAuthenticated: false, setIsAuthenticated: vi.fn() }),
}));

vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({
    links: { termsOfServiceUrl: 'https://kaitu.io/terms' },
  }),
}));

function renderDialog() {
  useLoginDialogStore.setState({
    isOpen: true,
    message: '',
    trigger: '',
    redirectPath: undefined,
  });
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <LoginDialog />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

describe('LoginDialog tabs', () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    const { cloudApi } = await import('../../services/cloud-api');
    (cloudApi.post as any).mockResolvedValue({
      code: 0,
      data: { accessToken: 'x', refreshToken: 'y' },
    });
  });

  it('renders code login tab by default', () => {
    const { container } = renderDialog();
    const tabs = container.querySelectorAll('[role="tab"]');
    expect(tabs.length).toBe(2);
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true');
    expect(tabs[1]).toHaveAttribute('aria-selected', 'false');
  });

  it('switches to password tab and submits to /api/auth/login/password', async () => {
    const { cloudApi } = await import('../../services/cloud-api');
    const { container } = renderDialog();

    // Pick tabs by [role="tab"] attribute, not getByRole — the latter
    // triggers dom-accessibility-api's accessible-name computation which
    // crashes on jsdom + MUI Tab DOM ("Cannot read properties of undefined
    // (reading 'getPropertyValue')"). Two tabs are rendered in fixed order
    // (code, password) per LoginDialog JSX.
    const tabs = container.querySelectorAll('[role="tab"]');
    expect(tabs.length).toBe(2);
    const passwordTab = tabs[1] as HTMLElement;
    fireEvent.click(passwordTab);

    // Use container-attribute queries instead of role-based lookups —
    // dom-accessibility-api's computeAccessibleName trips on jsdom +
    // MUI SVG icon nodes ("Cannot read properties of undefined
    // (reading 'getPropertyValue')") once Tabs + IconButton are present.
    const emailInput = container.querySelector(
      'input[autocomplete="email"]',
    ) as HTMLInputElement | null;
    expect(emailInput).not.toBeNull();

    const passwordInput = container.querySelector(
      'input[type="password"]',
    ) as HTMLInputElement | null;
    expect(passwordInput).not.toBeNull();

    fireEvent.change(emailInput!, { target: { value: 'a@b.com' } });
    fireEvent.change(passwordInput!, { target: { value: 'k7N#mq2P!xT9' } });

    // Submit by pressing Enter on the password field — avoids the same
    // accessible-name jsdom crash that getAllByRole('button') would hit.
    fireEvent.keyDown(passwordInput!, { key: 'Enter' });

    await waitFor(() =>
      expect(cloudApi.post).toHaveBeenCalledWith(
        '/api/auth/login/password',
        expect.objectContaining({ email: 'a@b.com', password: 'k7N#mq2P!xT9' }),
      ),
    );
  });
});
