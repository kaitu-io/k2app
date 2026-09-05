'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import NextLink from 'next/link';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { PlatformIcon } from '@/app/[locale]/install/platform-icons';
import { detectDevice, type DeviceType } from '@/lib/device-detection';
import { useBrand } from '@/hooks/useBrand';
import { Download, ExternalLink } from 'lucide-react';

/**
 * overleap 下载页（spec 2026-09-04-overleap-site-decoupling §3.3）。
 * 四张平台卡；无产物 / 未上架的平台显示 "Coming soon" 而不是 `Overleap_null_*` 死链。
 * 设备检测只做高亮与排序，不自动触发下载。
 */
export type InstallPlatform = 'windows' | 'macos' | 'ios' | 'android';

export interface InstallTarget {
  platform: InstallPlatform;
  /** 下载或商店链接；'' = 尚未提供。 */
  url: string;
  /** 桌面产物版本号（商店链接为空）。 */
  version?: string;
  /** 商店链接（App Store / Play）而非直接下载。 */
  store?: boolean;
}

const ORDER: InstallPlatform[] = ['windows', 'macos', 'ios', 'android'];

function toPlatform(d: DeviceType): InstallPlatform | null {
  return d === 'windows' || d === 'macos' || d === 'ios' || d === 'android' ? d : null;
}

export default function OverleapInstall({ targets }: { targets: InstallTarget[] }) {
  const t = useTranslations('download');
  const brand = useBrand();
  const [detected, setDetected] = useState<InstallPlatform | null>(null);

  useEffect(() => {
    setDetected(toPlatform(detectDevice().type));
  }, []);

  const byPlatform = new Map(targets.map((x) => [x.platform, x]));
  const ordered = detected ? [detected, ...ORDER.filter((p) => p !== detected)] : ORDER;
  const vars = { brand: brand.displayName };

  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-14">
      <div className="text-center mb-10">
        <h1 className="text-4xl font-bold mb-3">{t('download.title', vars)}</h1>
        <p className="text-muted-foreground max-w-2xl mx-auto">{t('download.subtitle')}</p>
        {detected && (
          <p className="mt-3 text-sm text-secondary" data-testid="detected-platform">
            {t('download.detected', { platform: t(`download.platforms.${detected}.name`) })}
          </p>
        )}
      </div>

      <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-5 mb-14">
        {ordered.map((platform) => {
          const target = byPlatform.get(platform) ?? { platform, url: '' };
          const name = t(`download.platforms.${platform}.name`);
          const available = Boolean(target.url);
          const featured = platform === detected;
          const label = !available
            ? t('download.comingSoon')
            : platform === 'ios'
              ? t('download.iosStore')
              : platform === 'android' && target.store
                ? t('download.androidStore')
                : t('download.getFor', { platform: name });
          return (
            <Card
              key={platform}
              data-testid={`install-card-${platform}`}
              data-available={available ? 'true' : 'false'}
              className={`p-6 flex flex-col bg-card ${featured ? 'border-primary shadow-[0_0_0_1px_var(--primary)]' : 'border-border'}`}
            >
              <div className="flex items-center gap-3 mb-4">
                <PlatformIcon type={platform} className="w-8 h-8" />
                <div>
                  <p className="font-semibold">{name}</p>
                  <p className="text-xs text-muted-foreground">{t(`download.platforms.${platform}.requirement`)}</p>
                </div>
              </div>
              <div className="mt-auto space-y-2">
                {available ? (
                  <Button asChild className="w-full font-semibold" variant={featured ? 'default' : 'outline'}>
                    <NextLink href={target.url} {...(target.store ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
                      {target.store ? <ExternalLink className="w-4 h-4 mr-2" /> : <Download className="w-4 h-4 mr-2" />}
                      {label}
                    </NextLink>
                  </Button>
                ) : (
                  <Button className="w-full font-semibold" variant="outline" disabled>
                    {label}
                  </Button>
                )}
                <p className="text-xs text-muted-foreground min-h-4">
                  {available
                    ? target.version && t('download.version', { version: target.version })
                    : t('download.comingSoonHint')}
                </p>
              </div>
            </Card>
          );
        })}
      </div>

      <section className="grid md:grid-cols-[1fr_auto] gap-8 items-start">
        <div>
          <h2 className="text-xl font-semibold mb-4">{t('download.next.title')}</h2>
          <ol className="space-y-3 text-sm text-foreground/90">
            {(['step1', 'step2', 'step3'] as const).map((k, i) => (
              <li key={k} className="flex gap-3">
                <span className="w-6 h-6 rounded-full bg-primary/15 text-primary text-xs font-bold flex items-center justify-center shrink-0">{i + 1}</span>
                <span>{t(`download.next.${k}`)}</span>
              </li>
            ))}
          </ol>
        </div>
        <div className="text-sm text-muted-foreground space-y-2 md:text-right">
          <p>
            {t('download.noSubscription')}{' '}
            <Link href="/purchase" className="text-primary hover:underline">{t('download.seePricing')}</Link>
          </p>
          <p>
            {t('download.help')}{' '}
            <Link href="/support" className="text-primary hover:underline">{t('download.helpLink')}</Link>
          </p>
        </div>
      </section>
    </div>
  );
}
