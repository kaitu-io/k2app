'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Download, ExternalLink } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { PlatformIcon } from '@/components/marketing/PlatformIcon';
import { detectPlatform } from '@/lib/device-detection';
import type { InstallPlatform, InstallTarget } from '@/lib/downloads';
import { SITE } from '@/lib/site';

const ORDER: InstallPlatform[] = ['windows', 'macos', 'ios', 'android'];
const NEXT_STEPS = ['step1', 'step2', 'step3'] as const;

/**
 * Four platform cards. Unpublished platforms show "Coming soon" instead of a dead
 * link. Device detection only highlights and reorders — it never starts a download.
 */
export default function InstallCards({ targets }: { targets: InstallTarget[] }) {
  const t = useTranslations('download');
  const [detected, setDetected] = useState<InstallPlatform | null>(null);

  useEffect(() => {
    setDetected(detectPlatform());
  }, []);

  const byPlatform = new Map(targets.map((x) => [x.platform, x]));
  const ordered = detected ? [detected, ...ORDER.filter((p) => p !== detected)] : ORDER;

  return (
    <div className="mx-auto max-w-5xl px-4 py-14 sm:px-6 lg:px-8">
      <div className="mb-10 text-center">
        <h1 className="mb-3 text-4xl font-bold">{t('title', { brand: SITE.name })}</h1>
        <p className="mx-auto max-w-2xl text-muted-foreground">{t('subtitle')}</p>
        {detected && (
          <p className="mt-3 text-sm text-secondary" data-testid="detected-platform">
            {t('detected', { platform: t(`platforms.${detected}.name`) })}
          </p>
        )}
      </div>

      <div className="mb-14 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
        {ordered.map((platform) => {
          const target = byPlatform.get(platform) ?? { platform, url: '' };
          const name = t(`platforms.${platform}.name`);
          const available = Boolean(target.url);
          const featured = platform === detected;
          const label = !available
            ? t('comingSoon')
            : platform === 'ios'
              ? t('iosStore')
              : platform === 'android' && target.store
                ? t('androidStore')
                : t('getFor', { platform: name });
          return (
            <Card
              key={platform}
              data-testid={`install-card-${platform}`}
              data-available={available ? 'true' : 'false'}
              className={`gap-0 bg-card p-6 ${featured ? 'border-primary shadow-[0_0_0_1px_var(--primary)]' : 'border-border'}`}
            >
              <div className="mb-4 flex items-center gap-3">
                <PlatformIcon type={platform} className="h-8 w-8" />
                <div>
                  <p className="font-semibold">{name}</p>
                  <p className="text-xs text-muted-foreground">{t(`platforms.${platform}.requirement`)}</p>
                </div>
              </div>
              <div className="mt-auto space-y-2">
                {available ? (
                  <Button asChild className="w-full font-semibold" variant={featured ? 'default' : 'outline'}>
                    <a href={target.url} {...(target.store ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
                      {target.store ? <ExternalLink /> : <Download />}
                      {label}
                    </a>
                  </Button>
                ) : (
                  <Button className="w-full font-semibold" variant="outline" disabled>
                    {label}
                  </Button>
                )}
                <p className="min-h-4 text-xs text-muted-foreground">
                  {available ? target.version && t('version', { version: target.version }) : t('comingSoonHint')}
                </p>
              </div>
            </Card>
          );
        })}
      </div>

      <section className="grid items-start gap-8 md:grid-cols-[1fr_auto]">
        <div>
          <h2 className="mb-4 text-xl font-semibold">{t('next.title')}</h2>
          <ol className="space-y-3 text-sm text-foreground/90">
            {NEXT_STEPS.map((k, i) => (
              <li key={k} className="flex gap-3">
                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/15 text-xs font-bold text-primary">{i + 1}</span>
                <span>{t(`next.${k}`)}</span>
              </li>
            ))}
          </ol>
        </div>
        <div className="space-y-2 text-sm text-muted-foreground md:text-end">
          <p>
            {t('noSubscription')}{' '}
            <Link href="/purchase" className="text-primary hover:underline">{t('seePricing')}</Link>
          </p>
          <p>
            {t('help')}{' '}
            <Link href="/support" className="text-primary hover:underline">{t('helpLink')}</Link>
          </p>
        </div>
      </section>
    </div>
  );
}
