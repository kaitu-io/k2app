/**
 * 版本比较工具
 * 支持语义化版本格式：1.2.3, 1.2.3-beta.1, 1.2.3+build.1 等
 */

export interface ParsedVersion {
  major: number;
  minor: number;
  patch: number;
  prerelease?: string;
  build?: string;
  raw: string;
}

/**
 * 解析版本字符串
 * @param version 版本字符串，如 "1.2.3-beta.1+build.123"
 * @returns 解析后的版本对象
 */
export function parseVersion(version: string): ParsedVersion {
  const cleanVersion = version.trim();
  
  // 匹配语义化版本格式: major.minor.patch[-prerelease][+build]
  const regex = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z\-\.]+))?(?:\+([0-9A-Za-z\-\.]+))?$/;
  const match = cleanVersion.match(regex);
  
  if (!match) {
    throw new Error(`Invalid version format: ${version}`);
  }
  
  const [, major, minor, patch, prerelease, build] = match;
  
  return {
    major: parseInt(major, 10),
    minor: parseInt(minor, 10),
    patch: parseInt(patch, 10),
    prerelease: prerelease || undefined,
    build: build || undefined,
    raw: cleanVersion,
  };
}

/**
 * 比较两个版本
 * @param version1 版本1
 * @param version2 版本2
 * @returns 
 *   1 如果 version1 > version2
 *   0 如果 version1 = version2
 *  -1 如果 version1 < version2
 */
export function compareVersions(version1: string, version2: string): number {
  try {
    const v1 = parseVersion(version1);
    const v2 = parseVersion(version2);
    
    // 比较主版本号
    if (v1.major !== v2.major) {
      return v1.major > v2.major ? 1 : -1;
    }
    
    // 比较次版本号
    if (v1.minor !== v2.minor) {
      return v1.minor > v2.minor ? 1 : -1;
    }
    
    // 比较补丁版本号
    if (v1.patch !== v2.patch) {
      return v1.patch > v2.patch ? 1 : -1;
    }
    
    // 比较预发布版本
    if (v1.prerelease && v2.prerelease) {
      // 都有预发布版本，按字符串比较
      return comparePrereleaseVersions(v1.prerelease, v2.prerelease);
    } else if (v1.prerelease && !v2.prerelease) {
      // v1 有预发布版本，v2 没有，v1 < v2
      return -1;
    } else if (!v1.prerelease && v2.prerelease) {
      // v1 没有预发布版本，v2 有，v1 > v2
      return 1;
    }
    
    // 版本相同
    return 0;
  } catch (error) {
    console.warn('Version comparison failed:', error);
    // 如果解析失败，按字符串比较
    return version1.localeCompare(version2);
  }
}

/**
 * 比较预发布版本
 * @param pre1 预发布版本1，如 "beta.1"
 * @param pre2 预发布版本2，如 "beta.2"
 */
function comparePrereleaseVersions(pre1: string, pre2: string): number {
  const parts1 = pre1.split('.');
  const parts2 = pre2.split('.');
  
  const maxLength = Math.max(parts1.length, parts2.length);
  
  for (let i = 0; i < maxLength; i++) {
    const part1 = parts1[i] || '';
    const part2 = parts2[i] || '';
    
    // 如果都是数字，按数字比较
    const num1 = parseInt(part1, 10);
    const num2 = parseInt(part2, 10);
    
    if (!isNaN(num1) && !isNaN(num2)) {
      if (num1 !== num2) {
        return num1 > num2 ? 1 : -1;
      }
    } else {
      // 按字符串比较
      const result = part1.localeCompare(part2);
      if (result !== 0) {
        return result;
      }
    }
  }
  
  return 0;
}

/**
 * 检查版本1是否比版本2新
 * @param version1 当前版本
 * @param version2 目标版本
 * @returns true 如果 version1 比 version2 新
 */
export function isNewerVersion(version1: string, version2: string): boolean {
  return compareVersions(version1, version2) > 0;
}

/**
 * 检查版本1是否比版本2旧
 * @param version1 当前版本
 * @param version2 目标版本
 * @returns true 如果 version1 比 version2 旧
 */
export function isOlderVersion(version1: string, version2: string): boolean {
  return compareVersions(version1, version2) < 0;
}

/**
 * 检查两个版本是否相同
 * @param version1 版本1
 * @param version2 版本2
 * @returns true 如果两个版本相同
 */
export function isSameVersion(version1: string, version2: string): boolean {
  return compareVersions(version1, version2) === 0;
}

/**
 * 验证版本字符串格式是否有效
 * @param version 版本字符串
 * @returns true 如果版本格式有效
 */
export function isValidVersion(version: string): boolean {
  try {
    parseVersion(version);
    return true;
  } catch {
    return false;
  }
}

/**
 * 格式化版本显示
 * @param version 版本字符串
 * @returns 格式化后的版本字符串
 */
export function formatVersion(version: string): string {
  try {
    const parsed = parseVersion(version);
    let formatted = `${parsed.major}.${parsed.minor}.${parsed.patch}`;
    
    if (parsed.prerelease) {
      formatted += `-${parsed.prerelease}`;
    }
    
    return formatted;
  } catch {
    return version;
  }
}

/**
 * 清理版本字符串，去除前缀如 "v"
 * @param version 原始版本字符串
 * @returns 清理后的版本字符串
 */
export function cleanVersion(version: string): string {
  return version.replace(/^v/i, '').trim();
}

/**
 * 检查服务版本是否需要更新
 * @param serviceVersion 服务版本
 * @param appVersion 应用版本
 * @returns 返回对象包含是否需要更新和版本信息
 */
export interface VersionCheckResult {
  needsUpdate: boolean;
  serviceVersion: string;
  appVersion: string;
  cleanServiceVersion: string;
  cleanAppVersion: string;
}

export function checkServiceVersionCompatibility(
  serviceVersion: string,
  appVersion: string
): VersionCheckResult {
  const cleanServiceVersion = cleanVersion(serviceVersion);
  const cleanAppVersion = cleanVersion(appVersion);
  
  // 使用 isSameVersion 进行精确比较
  const needsUpdate = !isSameVersion(cleanServiceVersion, cleanAppVersion);
  
  return {
    needsUpdate,
    serviceVersion,
    appVersion,
    cleanServiceVersion,
    cleanAppVersion,
  };
}

// 所有函数和类型已在上面直接导出