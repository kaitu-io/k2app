import { describe, it, expect, beforeAll } from 'vitest';
import i18n from '../../i18n/i18n';
import { getErrorMessage, ERROR_CODES } from '../errorCode';

const t = ((k: string, opts?: Record<string, unknown>) =>
  i18n.t(k, opts as never)) as unknown as Parameters<typeof getErrorMessage>[2];

describe('getErrorMessage — password strength', () => {
  beforeAll(async () => {
    if (!i18n.isInitialized) await i18n.init();
  });

  it('maps password_too_short to account.password.tooShort with length param', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'password_too_short', t);
    expect(msg).toBe(i18n.t('account:password.tooShort', { length: 10 }));
    expect(msg).toContain('10');
  });

  it('maps password_too_weak to account.password.tooWeak', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'password_too_weak', t);
    expect(msg).toBe(i18n.t('account:password.tooWeak'));
  });

  it('falls through to the default INVALID_ARGUMENT for other messages', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'some other reason', t);
    expect(msg).toBe(i18n.t('common:errors.client.invalidArgument'));
  });

  it('handles undefined message safely for INVALID_ARGUMENT', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, undefined, t);
    expect(msg).toBe(i18n.t('common:errors.client.invalidArgument'));
  });
});

describe('getErrorMessage — invalid credentials', () => {
  beforeAll(async () => {
    if (!i18n.isInitialized) await i18n.init();
  });

  // Wrong password / unknown email / no-password-set all return the SAME backend
  // code (400006, deliberately generic to prevent email enumeration). The message
  // must be specific enough to be actionable ("email or password is wrong") yet
  // NOT reveal which field failed — so it maps to auth.invalidCredentials, never
  // the vague auth.loginFailed (which reads like an unspecified/server error).
  it('maps INVALID_CREDENTIALS to auth.invalidCredentials (not the generic loginFailed)', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_CREDENTIALS, 'invalid email or password', t);
    expect(msg).toBe(i18n.t('auth:auth.invalidCredentials'));
    expect(msg).not.toBe(i18n.t('auth:auth.loginFailed'));
  });
});
