import type messages from '../../messages/en-GB/errors.json';
import { ApiError, ErrorCode } from './api';

export type ErrorKey = keyof typeof messages;

/**
 * User-facing text for any error thrown by lib/api.ts. Keys live in the
 * `errors` namespace. Backend `message` is never shown; it is only used to
 * tell apart enum payloads under InvalidArgument.
 */
export function errorMessage(err: unknown, t: (key: ErrorKey) => string): string {
  if (!(err instanceof ApiError)) return t('network');
  switch (err.code) {
    case ErrorCode.InvalidArgument:
      if (err.message === 'password_too_short') return t('passwordTooShort');
      if (err.message === 'password_too_weak') return t('passwordTooWeak');
      return t('invalidArgument');
    case ErrorCode.InvalidVerificationCode:
      return t('invalidVerificationCode');
    case ErrorCode.VerificationCodeExpired:
      return t('verificationCodeExpired');
    case ErrorCode.InvalidCredentials:
      return t('invalidCredentials');
    case ErrorCode.TooManyRequests:
      return t('tooManyRequests');
    case ErrorCode.NotLogin:
      return t('unauthorized');
    case ErrorCode.ChannelUnavailable:
    case ErrorCode.ServiceUnavailable:
      return t('serviceUnavailable');
    default:
      return t('generic');
  }
}
