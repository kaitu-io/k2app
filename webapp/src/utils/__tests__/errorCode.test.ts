import { describe, it, expect, beforeAll } from 'vitest';
import i18n, { i18nPromise } from '../../i18n/i18n';
import { getErrorMessage, ERROR_CODES } from '../errorCode';

const t = ((k: string, opts?: Record<string, unknown>) =>
  i18n.t(k, opts as never)) as unknown as Parameters<typeof getErrorMessage>[2];

describe('getErrorMessage — password strength', () => {
  beforeAll(async () => {
    // Await the module's own init — never race it with a bare i18n.init().
    // i18n.ts fires initI18n() at import time, and it awaits preloadResources()
    // before calling .init(). A guard like `if (!i18n.isInitialized) i18n.init()`
    // therefore wins whenever preload is slow (e.g. under CPU contention) and
    // initializes the singleton with NO resources, permanently. Every t() then
    // returns its default/key and the assertions below fail — a pure timing
    // flake that looks like a broken test.
    await i18nPromise;
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

describe('getErrorMessage — email already in use', () => {
  beforeAll(async () => {
    await i18nPromise;
  });

  // The backend rejects a bind/update-email request whose target address already
  // belongs to another account BEFORE issuing or mailing any verification code.
  // It used to share the generic 422, which rendered as "参数错误" — indistinguishable
  // from a transient glitch, so users kept re-tapping "send code" and reported it
  // as "收不到验证码" (support case 2026-07-29). The dedicated 409001 must resolve
  // to copy that names the actual cause.
  it('maps EMAIL_ALREADY_IN_USE to the dedicated i18n copy', () => {
    const msg = getErrorMessage(ERROR_CODES.EMAIL_ALREADY_IN_USE, 'email already in use', t);
    expect(msg).toBe(i18n.t('auth:updateEmail.emailAlreadyInUse'));
  });

  // Mutation guards: the whole point of the fix is NOT landing on either generic
  // string. Both would pass a naive "returns something" assertion.
  it('does not fall back to the generic invalidArgument or sendCodeFailed copy', () => {
    const msg = getErrorMessage(ERROR_CODES.EMAIL_ALREADY_IN_USE, 'email already in use', t);
    expect(msg).not.toBe(i18n.t('common:errors.client.invalidArgument'));
    expect(msg).not.toBe(i18n.t('auth:updateEmail.sendCodeFailed'));
  });

  // A missing translation key makes i18next echo the key itself — that would slip
  // past the assertions above while shipping "auth:updateEmail.emailAlreadyInUse"
  // to the user's screen.
  it('resolves to real copy, not the raw i18n key', () => {
    const msg = getErrorMessage(ERROR_CODES.EMAIL_ALREADY_IN_USE, 'email already in use', t);
    expect(msg).not.toContain('updateEmail.emailAlreadyInUse');
    expect(msg.length).toBeGreaterThan(4);
  });
});
