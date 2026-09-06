/**
 * LoginDialog brand / purchase gates.
 *
 *  - Invite code (alert + field, shown to a not-yet-activated user after the
 *    code is sent) is a kaitu-only program: gated on features.invite.
 *  - "Activate Service" navigates to /purchase, which App.tsx registers only
 *    when purchaseSurfaceAvailable() — so the button consults the same gate
 *    (Play-only Android on overleap must never show it).
 *
 * Mock set mirrors LoginDialog.test.tsx (MUI Dialog subtree stubbed for jsdom).
 * Each brand runs the case that applies to it; expectations go through i18n.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';

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
import { brandConfig } from '../../brands';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: vi.fn() },
}));
vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid'),
}));
vi.mock('../../services/cache-store', () => ({
  cacheStore: { clear: vi.fn(), get: vi.fn(() => null), subscribe: vi.fn(() => vi.fn()) },
}));
vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({ isAuthenticated: false, setIsAuthenticated: vi.fn() }),
}));
vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({ links: { termsOfServiceUrl: 'https://example.com/terms' } }),
}));

const tx = (key: string) => i18n.t(key);
const ACTIVATE = 'auth:auth.activateService';
const INVITE_ALERT = 'auth:auth.inviteCodeOptional';
const INVITE_LABEL = 'auth:auth.inviteCode';

function renderDialog(os: string) {
  (window as any)._platform = { os, version: '0.4.10', openExternal: vi.fn() };
  useLoginDialogStore.setState({ isOpen: true, message: '', trigger: '', redirectPath: undefined });
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <LoginDialog />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

/** Drive the dialog into "code sent, user not yet activated". */
async function sendCodeForUnactivatedUser(container: HTMLElement) {
  const { cloudApi } = await import('../../services/cloud-api');
  (cloudApi.post as any).mockResolvedValue({
    code: 0,
    data: { userExists: false, isActivated: false, isFirstOrderDone: false },
  });
  const emailInput = container.querySelector('input[type="email"], input[autocomplete="email"]') as HTMLInputElement | null;
  expect(emailInput).not.toBeNull();
  fireEvent.change(emailInput!, { target: { value: 'a@b.com' } });
  fireEvent.click(screen.getByText(tx('auth:auth.sendCode')));
  await waitFor(() =>
    expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/code', expect.objectContaining({ email: 'a@b.com' })),
  );
  // Step 2 is up once the one-time-code input renders.
  await waitFor(() => expect(container.querySelector('input[autocomplete="one-time-code"]')).not.toBeNull());
}

describe('LoginDialog — brand / purchase gates', () => {
  beforeEach(() => { vi.clearAllMocks(); });
  afterEach(() => { delete (window as any)._platform; });

  it('translations are loaded — the absence assertions below are meaningful', () => {
    for (const k of [ACTIVATE, INVITE_ALERT, INVITE_LABEL]) expect(tx(k)).not.toBe(k.split(':')[1]);
  });

  it.skipIf(brandConfig.id !== 'overleap')('overleap: no invite-code alert or field after the code is sent', async () => {
    const { container } = renderDialog('macos');
    await sendCodeForUnactivatedUser(container);
    expect(screen.queryByText(tx(INVITE_ALERT))).toBeNull();
    expect(screen.queryByText(tx(INVITE_LABEL))).toBeNull();
    expect(container.querySelector('input[maxlength="8"]')).toBeNull();
  });

  it.skipIf(brandConfig.id !== 'kaitu')('kaitu: invite-code alert and field are shown to a not-yet-activated user', async () => {
    const { container } = renderDialog('macos');
    await sendCodeForUnactivatedUser(container);
    expect(screen.getByText(tx(INVITE_ALERT))).toBeTruthy();
    expect(container.querySelector('input[maxlength="8"]')).not.toBeNull();
  });

  it('desktop: Activate Service is offered (purchase surface available)', () => {
    renderDialog('macos');
    expect(screen.getByText(tx(ACTIVATE))).toBeTruthy();
  });

  it.skipIf(brandConfig.id !== 'overleap')('overleap on Android: Activate Service is hidden (Play-only, /purchase unregistered)', () => {
    renderDialog('android');
    expect(screen.queryByText(tx(ACTIVATE))).toBeNull();
  });

  it.skipIf(brandConfig.id !== 'kaitu')('kaitu on Android: Activate Service is still offered', () => {
    renderDialog('android');
    expect(screen.getByText(tx(ACTIVATE))).toBeTruthy();
  });
});
