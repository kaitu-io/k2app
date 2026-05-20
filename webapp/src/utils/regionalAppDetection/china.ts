/**
 * Detect Android apps likely to belong to a Chinese app ecosystem so they can
 * be auto-routed direct in smart-routing (chnroute) mode.
 *
 * Two signals:
 *   - installer  Strong: PackageManager.getInstallSourceInfo() reported a
 *                known Chinese app store as the installer.
 *   - prefix     Heuristic: package name uses a namespace owned by a known
 *                Chinese company. Kept tight to minimise false positives.
 *
 * The prefix list intentionally excludes broad TLD reversals like `cn.*`
 * (many overseas apps publish Chinese variants), Google's `com.android.*`
 * and `com.google.*` (not Chinese), and Samsung / Facebook namespaces.
 */
import type { AppDetector, AutoDetectedAppEntry } from './types';

export const REASON_INSTALLER_KEY = 'dashboard:appBypass.cn.reasonInstaller';
export const REASON_PREFIX_KEY = 'dashboard:appBypass.cn.reasonPrefix';

const CHINESE_INSTALLERS: ReadonlySet<string> = new Set([
  'com.xiaomi.market',
  'com.huawei.appmarket',
  'com.bbk.appstore',
  'com.oppo.market',
  'com.heytap.market',
  'com.tencent.android.qqdownloader',
  'com.qihoo.appstore',
  'com.baidu.appsearch',
  'com.wandoujia.phoenix2',
  'com.lenovo.leos.appstore',
  'com.coolapk.market',
]);

/**
 * Entries ending in '.' are prefix-only (`com.tencent.` matches `com.tencent.mm`).
 * Entries without a trailing '.' are exact-only.
 */
const CHINESE_PACKAGE_PREFIXES: readonly string[] = [
  // Tier-1 internet
  'com.tencent.',
  'com.alipay.',
  'com.alibaba.',
  'com.taobao.',
  'com.tmall.',
  'com.baidu.',
  'com.bytedance.',
  'com.ss.android.',
  'com.netease.',
  'com.youdao.',
  'com.sina.',
  'com.weibo.',
  'com.qihoo.',
  'com.qihoo360.',
  // OEM Chinese-side
  'com.miui.',
  'com.xiaomi.',
  'com.huawei.',
  'com.bbk.',
  'com.oppo.',
  'com.heytap.',
  // Tools / content
  'com.iflytek.',
  'com.autonavi.',
  'com.amap.',
  'com.qiyi.',
  'com.iqiyi.',
  'com.youku.',
  'com.bilibili.',
  'com.zhihu.',
  // Lifestyle / transport
  'com.meituan.',
  'com.dianping.',
  'com.didi.',
  'com.didichuxing.',
  'com.kuaishou.',
  // E-commerce
  'com.jingdong.',
  'com.jd.',
  // Banking
  'com.cmbchina.',
  'cmb.',
  'com.icbc.',
  'com.ICBC.',
  'com.ccb.',
  'com.boc.',
  // Singletons (exact match, no trailing dot)
  'com.eg.android.AlipayGphone',
  'com.UCMobile',
  'com.smile.gifmaker',
];

function matchesChinesePrefix(pkg: string): boolean {
  if (!pkg) return false;
  for (const entry of CHINESE_PACKAGE_PREFIXES) {
    if (entry.endsWith('.')) {
      if (pkg.startsWith(entry)) return true;
    } else if (pkg === entry) {
      return true;
    }
  }
  return false;
}

/**
 * Returns the i18n reason key for this package, or null when it doesn't
 * look Chinese. Installer signal wins over prefix when both apply — it is
 * the more authoritative source.
 */
export function detectChineseReasonKey(
  packageName: string,
  installerPackageName: string | null | undefined,
): string | null {
  if (installerPackageName && CHINESE_INSTALLERS.has(installerPackageName)) {
    return REASON_INSTALLER_KEY;
  }
  if (matchesChinesePrefix(packageName)) {
    return REASON_PREFIX_KEY;
  }
  return null;
}

export const chinaDetector: AppDetector = {
  region: 'cn',
  sectionTitleKey: 'dashboard:appBypass.cn.section',
  noteSmartKey: 'dashboard:appBypass.cn.noteSmart',
  noteGlobalKey: 'dashboard:appBypass.cn.noteGlobal',
  detect(apps) {
    const detected: AutoDetectedAppEntry[] = [];
    for (const app of apps) {
      const reasonKey = detectChineseReasonKey(app.packageName, app.installerPackageName ?? null);
      if (reasonKey !== null) {
        detected.push({
          packageName: app.packageName,
          label: app.label,
          iconUrl: app.iconUrl,
          reasonKey,
        });
      }
    }
    detected.sort((a, b) => a.label.localeCompare(b.label, 'zh-Hans-CN'));
    return detected;
  },
};
