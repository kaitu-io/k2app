import { ErrorCode } from './api';

/**
 * Map API error code to i18n error message.
 * Use in public [locale] pages with next-intl's useTranslations().
 */
export function getApiErrorMessage(
  code: number,
  t: (key: string) => string,
  fallback?: string
): string {
  switch (code) {
    case ErrorCode.InvalidOperation:
      return t('errors.badRequest');
    case ErrorCode.NotLogin:
      return t('errors.unauthorized');
    case ErrorCode.PaymentRequired:
      return t('errors.paymentRequired');
    case ErrorCode.Forbidden:
      return t('errors.forbidden');
    case ErrorCode.NotFound:
      return t('errors.notFound');
    case ErrorCode.NotSupported:
      return t('errors.notSupported');
    case ErrorCode.UpgradeRequired:
      return t('errors.upgradeRequired');
    case ErrorCode.Conflict:
      return t('errors.conflict');
    case ErrorCode.InvalidArgument:
      return t('errors.invalidArgument');
    case ErrorCode.TooEarly:
      return t('errors.tooEarly');
    case ErrorCode.TooManyRequests:
      return t('errors.tooManyRequests');
    case ErrorCode.SystemError:
      return t('errors.serverError');
    case ErrorCode.ServiceUnavailable:
      return t('errors.serviceUnavailable');
    case ErrorCode.InvalidCampaignCode:
      return t('errors.invalidCampaignCode');
    case ErrorCode.InvalidClientClock:
      return t('errors.invalidClock');
    case ErrorCode.InvalidVerificationCode:
      return t('errors.invalidVerificationCode');
    case ErrorCode.InvalidInviteCode:
      return t('errors.invalidInviteCode');
    case ErrorCode.SelfInvitation:
      return t('errors.selfInvitation');
    case ErrorCode.InvalidCredentials:
      return t('errors.invalidCredentials');
    default:
      return fallback || t('errors.unknown');
  }
}

const zhMessages: Record<number, string> = {
  [ErrorCode.InvalidOperation]: '请求参数错误',
  [ErrorCode.NotLogin]: '请先登录',
  [ErrorCode.PaymentRequired]: '会员已过期，请续费',
  [ErrorCode.Forbidden]: '没有权限执行此操作',
  [ErrorCode.NotFound]: '请求的资源不存在',
  [ErrorCode.NotSupported]: '功能不支持',
  [ErrorCode.UpgradeRequired]: '请升级到最新版本',
  [ErrorCode.Conflict]: '操作冲突，请重试',
  [ErrorCode.InvalidArgument]: '参数错误',
  [ErrorCode.TooEarly]: '请稍后再试',
  [ErrorCode.TooManyRequests]: '请求过于频繁，请稍后重试',
  [ErrorCode.SystemError]: '服务器错误，请稍后重试',
  [ErrorCode.ServiceUnavailable]: '服务器不可用，请稍后重试',
  [ErrorCode.InvalidCampaignCode]: '活动码无效',
  [ErrorCode.InvalidClientClock]: '设备时间不正确，请校准系统时间',
  [ErrorCode.InvalidVerificationCode]: '验证码错误',
  [ErrorCode.InvalidInviteCode]: '邀请码不正确',
  [ErrorCode.SelfInvitation]: '不能使用自己的邀请码',
  [ErrorCode.InvalidCredentials]: '登录凭证无效',
};

/**
 * Map API error code to Chinese error message.
 * Use in manager pages (Chinese-only, no i18n context).
 */
export function getApiErrorMessageZh(code: number, fallback?: string): string {
  return zhMessages[code] || fallback || '操作失败，请重试';
}
