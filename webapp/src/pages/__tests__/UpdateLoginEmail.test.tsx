import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n, { i18nPromise } from '../../i18n/i18n';
import { ERROR_CODES } from '../../utils/errorCode';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: vi.fn() },
}));

import { cloudApi } from '../../services/cloud-api';
import UpdateLoginEmail from '../UpdateLoginEmail';

const mockPost = cloudApi.post as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => {
  // clearAllMocks wipes implementations too — re-arm per test (webapp/CLAUDE.md).
  vi.clearAllMocks();
});

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <UpdateLoginEmail />
      </MemoryRouter>
    </I18nextProvider>
  );
}

/** Fills the email field and taps "send code". */
async function submitEmail(value: string) {
  const input = screen.getAllByRole('textbox')[0];
  fireEvent.change(input, { target: { value } });
  const sendButton = screen
    .getAllByRole('button')
    .find((b) => b.textContent === i18n.t('auth:updateEmail.sendCode'));
  expect(sendButton, 'send-code button must be findable').toBeDefined();
  fireEvent.click(sendButton!);
}

// End-to-end of the *frontend* chain for the 409001 fix: cloudApi response →
// handleResponseError → getErrorMessage → i18n → setError → rendered <Alert>.
// The unit test in utils/__tests__/errorCode.test.ts only covers the mapping
// function; this pins that the string actually reaches the screen.
describe('UpdateLoginEmail — email already in use (409001)', () => {
  beforeEach(async () => {
    await i18nPromise;
  });

  it('renders the dedicated copy when the backend reports 409001', async () => {
    mockPost.mockResolvedValue({ code: ERROR_CODES.EMAIL_ALREADY_IN_USE, message: 'email already in use' });
    renderPage();
    await submitEmail('237875618@qq.com');

    await waitFor(() => {
      expect(screen.getByText(i18n.t('auth:updateEmail.emailAlreadyInUse'))).toBeInTheDocument();
    });
  });

  it('does not show the retry-flavoured fallback copy', async () => {
    mockPost.mockResolvedValue({ code: ERROR_CODES.EMAIL_ALREADY_IN_USE, message: 'email already in use' });
    renderPage();
    await submitEmail('237875618@qq.com');

    // "验证码发送失败，请稍后重试" is what an OLD client shows for this code (it
    // falls through getErrorMessage's default to the caller's defaultMessage).
    // Telling the user to retry is exactly the wrong instruction here — nothing
    // was ever sent and retrying can never succeed.
    await waitFor(() => {
      expect(screen.getByText(i18n.t('auth:updateEmail.emailAlreadyInUse'))).toBeInTheDocument();
    });
    expect(screen.queryByText(i18n.t('auth:updateEmail.sendCodeFailed'))).not.toBeInTheDocument();
  });

  it('does not enter the "code sent" success state', async () => {
    mockPost.mockResolvedValue({ code: ERROR_CODES.EMAIL_ALREADY_IN_USE, message: 'email already in use' });
    renderPage();
    await submitEmail('237875618@qq.com');

    // handleResponseError throws before setSuccess(true), so the "code sent"
    // confirmation must never appear on a rejection.
    await waitFor(() => {
      expect(screen.getByText(i18n.t('auth:updateEmail.emailAlreadyInUse'))).toBeInTheDocument();
    });
    expect(screen.queryByText(i18n.t('auth:updateEmail.codeSentSuccess'))).not.toBeInTheDocument();
  });

  it('still shows the generic failure copy for an unrelated error code', async () => {
    // Control case: proves the dedicated copy is code-driven, not shown for any
    // failure whatsoever.
    mockPost.mockResolvedValue({ code: ERROR_CODES.INTERNAL_SERVER_ERROR, message: 'boom' });
    renderPage();
    await submitEmail('237875618@qq.com');

    await waitFor(() => {
      expect(screen.getByText(i18n.t('common:common.serverError'))).toBeInTheDocument();
    });
    expect(screen.queryByText(i18n.t('auth:updateEmail.emailAlreadyInUse'))).not.toBeInTheDocument();
  });
});
