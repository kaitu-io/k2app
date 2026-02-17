/**
 * 检查用户是否已过期
 * @param expiredAt 过期时间字符串
 * @returns 是否已过期
 */
export function isExpired(expiredAt?: number): boolean {
  if (!expiredAt) return false;
  return new Date(expiredAt * 1000) < new Date();
} 