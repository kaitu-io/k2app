import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ChangePasswordDialog from '../ChangePasswordDialog';

vi.mock('@/lib/api', () => ({
  api: { setPassword: vi.fn().mockResolvedValue(undefined) },
  ApiError: class extends Error {
    code = 0;
  },
  ErrorCode: { InvalidArgument: 422 },
}));

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

beforeEach(() => vi.clearAllMocks());

function renderDialog(
  props: Partial<React.ComponentProps<typeof ChangePasswordDialog>> = {},
) {
  return render(
    <ChangePasswordDialog
      open={true}
      onOpenChange={vi.fn()}
      hasPassword={false}
      userEmail="a@b.com"
      {...props}
    />,
  );
}

describe('ChangePasswordDialog', () => {
  it('shows set-password title when hasPassword=false', () => {
    renderDialog({ hasPassword: false });
    expect(
      screen.getByText(/admin\.account\.password\.setPassword|设置密码|Set password/i),
    ).toBeInTheDocument();
  });

  it('shows change-password title when hasPassword=true', () => {
    renderDialog({ hasPassword: true });
    expect(
      screen.getByText(/admin\.account\.password\.changePassword|修改密码|Change password/i),
    ).toBeInTheDocument();
  });

  it('disables submit when password is too short', async () => {
    renderDialog();
    const newPw = document.getElementById('change-pw-new') as HTMLInputElement;
    fireEvent.change(newPw, { target: { value: 'short' } });
    await waitFor(() => {
      const submit = screen
        .getAllByRole('button')
        .find((b) => b.textContent && /common\.confirm|确认|Confirm/i.test(b.textContent));
      expect(submit).toBeDisabled();
    });
  });

  it('submits to api.setPassword when valid', async () => {
    const { api } = await import('@/lib/api');
    renderDialog();
    const newPw = document.getElementById('change-pw-new') as HTMLInputElement;
    const confirm = document.getElementById('change-pw-confirm') as HTMLInputElement;
    fireEvent.change(newPw, { target: { value: 'k7N#mq2P!xT9' } });
    fireEvent.change(confirm, { target: { value: 'k7N#mq2P!xT9' } });
    await waitFor(
      () => {
        const submit = screen
          .getAllByRole('button')
          .find((b) => b.textContent && /common\.confirm|确认|Confirm/i.test(b.textContent));
        expect(submit).not.toBeDisabled();
      },
      { timeout: 3000 },
    );
    const submit = screen
      .getAllByRole('button')
      .find((b) => b.textContent && /common\.confirm|确认|Confirm/i.test(b.textContent));
    fireEvent.click(submit!);
    await waitFor(() => expect(api.setPassword).toHaveBeenCalled());
  });
});
