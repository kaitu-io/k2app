import { describe, it, expect, beforeAll } from 'vitest';
import i18n from '../../i18n/i18n';
import { getErrorMessage, ERROR_CODES } from '../errorCode';

const t = ((k: string) => i18n.t(k)) as unknown as Parameters<typeof getErrorMessage>[2];

describe('getErrorMessage — password strength', () => {
  beforeAll(async () => {
    if (!i18n.isInitialized) await i18n.init();
  });

  it('maps password_too_short to account.password.tooShort', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'password_too_short', t);
    expect(msg).toBe(i18n.t('account:password.tooShort'));
  });

  it('maps password_too_weak to account.password.tooWeak', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'password_too_weak', t);
    expect(msg).toBe(i18n.t('account:password.tooWeak'));
  });

  it('falls through to default INVALID_ARGUMENT for other messages', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, 'some other reason', t);
    expect(msg).not.toBe(i18n.t('account:password.tooShort'));
    expect(msg).not.toBe(i18n.t('account:password.tooWeak'));
  });

  it('handles undefined message safely for INVALID_ARGUMENT', () => {
    const msg = getErrorMessage(ERROR_CODES.INVALID_ARGUMENT, undefined, t);
    expect(msg).not.toBe(i18n.t('account:password.tooShort'));
    expect(msg).not.toBe(i18n.t('account:password.tooWeak'));
  });
});
