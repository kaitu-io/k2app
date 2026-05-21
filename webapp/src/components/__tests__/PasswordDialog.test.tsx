import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom). Mirrors the
// pattern used in LoginDialog.test.tsx.
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

import PasswordDialog from '../PasswordDialog';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: vi.fn().mockResolvedValue({ code: 0 }) },
}));

beforeEach(() => vi.clearAllMocks());

function renderDialog(overrides: Partial<React.ComponentProps<typeof PasswordDialog>> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <PasswordDialog
        open={true}
        hasPassword={false}
        userEmail="a@b.com"
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        {...overrides}
      />
    </I18nextProvider>
  );
}

describe('PasswordDialog', () => {
  it('shows "set password" title when hasPassword=false', () => {
    renderDialog({ hasPassword: false });
    // title text in any of 7 locales — match flexible
    expect(screen.getByText(/设置密码|Set password|パスワードを設定|設定密碼/i)).toBeInTheDocument();
  });

  it('shows "change password" title when hasPassword=true', () => {
    renderDialog({ hasPassword: true });
    expect(screen.getByText(/修改密码|Change password|パスワードを変更|修改密碼/i)).toBeInTheDocument();
  });

  it('disables submit button when password is empty', () => {
    renderDialog();
    const buttons = screen.getAllByRole('button');
    const submit = buttons.find((b) => b.textContent && /确认|确定|confirm|ok/i.test(b.textContent));
    expect(submit).toBeDisabled();
  });

  it('disables submit when password is too short', async () => {
    renderDialog();
    const newPw = screen.getByLabelText(/new password|新密码|新密碼|新しいパスワード/i) as HTMLInputElement;
    const confirm = screen.getByLabelText(/confirm password|确认密码|確認密碼|パスワードを確認/i) as HTMLInputElement;
    fireEvent.change(newPw, { target: { value: 'short' } });
    fireEvent.change(confirm, { target: { value: 'short' } });

    // Wait for the strength check to settle (it's async — zxcvbn lazy-loads)
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      const submit = buttons.find((b) => b.textContent && /确认|确定|confirm|ok/i.test(b.textContent));
      expect(submit).toBeDisabled();
    });
  });

  it('renders the strength meter once user types', async () => {
    renderDialog();
    const newPw = screen.getByLabelText(/new password|新密码|新密碼|新しいパスワード/i) as HTMLInputElement;
    fireEvent.change(newPw, { target: { value: 'somepassword' } });
    await waitFor(() => {
      expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });
  });

  it('enables submit when password is strong AND confirmed matches', async () => {
    renderDialog();
    const newPw = screen.getByLabelText(/new password|新密码|新密碼|新しいパスワード/i) as HTMLInputElement;
    const confirm = screen.getByLabelText(/confirm password|确认密码|確認密碼|パスワードを確認/i) as HTMLInputElement;
    fireEvent.change(newPw, { target: { value: 'k7N#mq2P!xT9' } });
    fireEvent.change(confirm, { target: { value: 'k7N#mq2P!xT9' } });
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      const submit = buttons.find((b) => b.textContent && /确认|确定|confirm|ok/i.test(b.textContent));
      expect(submit).not.toBeDisabled();
    });
  });
});
