import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';
import PasswordAuthFields from '../PasswordAuthFields';

function renderFields(overrides?: Partial<React.ComponentProps<typeof PasswordAuthFields>>) {
  const props = {
    email: '',
    password: '',
    onEmailChange: vi.fn(),
    onPasswordChange: vi.fn(),
    onSubmit: vi.fn(),
    isSubmitting: false,
    emailSuggestion: null,
    onAcceptSuggestion: vi.fn(),
    onEmailBlur: vi.fn(),
    ...overrides,
  };
  render(<I18nextProvider i18n={i18n}><PasswordAuthFields {...props} /></I18nextProvider>);
  return props;
}

describe('PasswordAuthFields', () => {
  it('disables submit when fields are empty', () => {
    renderFields();
    const buttons = screen.getAllByRole('button');
    const submit = buttons.find((b) => b.textContent && /login|登录|登入|ログイン/i.test(b.textContent));
    expect(submit).toBeDefined();
    expect(submit).toBeDisabled();
  });

  it('enables submit when email is valid and password filled', () => {
    renderFields({ email: 'a@b.com', password: 'k7N#mq2P!xT9' });
    const buttons = screen.getAllByRole('button');
    const submit = buttons.find((b) => b.textContent && /login|登录|登入|ログイン/i.test(b.textContent));
    expect(submit).not.toBeDisabled();
  });

  it('disables submit while isSubmitting is true even if filled', () => {
    renderFields({ email: 'a@b.com', password: 'k7N#mq2P!xT9', isSubmitting: true });
    const buttons = screen.getAllByRole('button');
    const submit = buttons.find((b) => b.textContent && /login|登录|登入|ログイン/i.test(b.textContent));
    expect(submit).toBeDisabled();
  });

  it('fires onSubmit when Enter pressed in password field with valid state', () => {
    const props = renderFields({ email: 'a@b.com', password: 'k7N#mq2P!xT9' });
    const pwInput = screen.getByLabelText(/password|密码|密碼|パスワード/i) as HTMLInputElement;
    fireEvent.keyDown(pwInput, { key: 'Enter' });
    expect(props.onSubmit).toHaveBeenCalledTimes(1);
  });

  it('does not fire onSubmit on Enter when email is invalid', () => {
    const props = renderFields({ email: 'not-an-email', password: 'k7N#mq2P!xT9' });
    const pwInput = screen.getByLabelText(/password|密码|密碼|パスワード/i) as HTMLInputElement;
    fireEvent.keyDown(pwInput, { key: 'Enter' });
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it('does not render a password strength meter (login form)', () => {
    renderFields({ password: 'k7N#mq2P!xT9' });
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('clicking submit invokes onSubmit', () => {
    const props = renderFields({ email: 'a@b.com', password: 'k7N#mq2P!xT9' });
    const buttons = screen.getAllByRole('button');
    const submit = buttons.find((b) => b.textContent && /login|登录|登入|ログイン/i.test(b.textContent));
    fireEvent.click(submit!);
    expect(props.onSubmit).toHaveBeenCalledTimes(1);
  });

  it('forwards onEmailChange with the typed value', () => {
    const props = renderFields();
    const emailInput = screen.getAllByRole('textbox')[0] as HTMLInputElement;
    fireEvent.change(emailInput, { target: { value: 'typed@example.com' } });
    expect(props.onEmailChange).toHaveBeenCalledWith('typed@example.com');
  });

  it('forwards onPasswordChange with the typed value', () => {
    const props = renderFields();
    const pwInput = screen.getByLabelText(/password|密码|密碼|パスワード/i) as HTMLInputElement;
    fireEvent.change(pwInput, { target: { value: 'p@ssw0rd!23' } });
    expect(props.onPasswordChange).toHaveBeenCalledWith('p@ssw0rd!23');
  });

  it('fires onEmailBlur when email field loses focus', () => {
    const props = renderFields({ email: 'a@b.com' });
    const emailInput = screen.getAllByRole('textbox')[0] as HTMLInputElement;
    fireEvent.blur(emailInput);
    expect(props.onEmailBlur).toHaveBeenCalledTimes(1);
  });

  it('renders EmailSuggestion when emailSuggestion is non-null and accepts it', () => {
    const props = renderFields({ emailSuggestion: 'fixed@example.com', email: 'fixed@example.con' });
    // EmailSuggestion renders the accept action as a MUI Link rendered as a
    // <button>. Use getByText with the i18n "Use suggestion" label across
    // locales — getByRole('button', {name}) trips on jsdom + MUI Link in
    // this test env (dom-accessibility-api getComputedStyle quirk).
    const acceptButton = screen.getByText(/use suggestion|使用建议|使用建議|修正する/i);
    fireEvent.click(acceptButton);
    expect(props.onAcceptSuggestion).toHaveBeenCalledTimes(1);
  });
});
