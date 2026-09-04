import type { InstallTarget } from '@/components/install-overleap/OverleapInstall';
import type { AllDownloadLinks } from './downloads';
import type { Brand } from './brands';

/**
 * 把 CDN 清单 + 品牌商店链接折成四张平台卡的目标。缺产物 / 未上架 → url ''（"Coming soon"），
 * 绝不拼出 `Overleap_null_*` 这种死链。iOS 优先商店链接（CDN 清单里的 appstore_url 也是商店），
 * Android 优先 Play，没有 Play 时才给 APK。
 */
export function buildInstallTargets(all: AllDownloadLinks, brand: Brand): InstallTarget[] {
  const desktop = all.desktop.stable ?? all.desktop.beta;
  const ios = brand.storeLinks.ios || all.mobile?.ios.url || '';
  const android = brand.storeLinks.android || all.mobile?.android.primary || '';
  return [
    { platform: 'windows', url: desktop?.links.windows.primary ?? '', version: desktop?.version },
    { platform: 'macos', url: desktop?.links.macos.primary ?? '', version: desktop?.version },
    { platform: 'ios', url: ios, store: true },
    { platform: 'android', url: android, store: Boolean(brand.storeLinks.android), version: all.mobile?.android.version },
  ];
}

