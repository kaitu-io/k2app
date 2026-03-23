'use client';

import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Gift, AlertCircle, Clock } from 'lucide-react';
import type { LicenseKeyPublic } from '@/lib/api';

interface RedeemClientProps {
  initialKey: LicenseKeyPublic | null;
  uuid: string;
}

function getDaysRemaining(expiresAt: number): number {
  const now = Math.floor(Date.now() / 1000);
  const diff = expiresAt - now;
  return Math.max(0, Math.ceil(diff / 86400));
}

export default function RedeemClient({ initialKey, uuid }: RedeemClientProps) {
  const t = useTranslations('licenseKeys');

  // Used state
  if (initialKey?.isUsed) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">
            {t('gift.used')}
          </h1>
          <p className="text-muted-foreground mb-8">
            {t('gift.subtitle')}
          </p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Expired state
  if (initialKey?.isExpired) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <Clock className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">
            {t('gift.expired')}
          </h1>
          <p className="text-muted-foreground mb-8">
            {t('gift.subtitle')}
          </p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Valid gift card
  const key = initialKey;
  if (!key) return null;

  const daysRemaining = getDaysRemaining(key.expiresAt);
  const ctaHref = `/purchase?licenseKey=${uuid}`;

  const discountDisplay =
    key.discountType === 'discount'
      ? t('gift.discount', { value: key.discountValue })
      : t('gift.coupon', { value: key.discountValue });

  return (
    <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
      {/* Hero icon */}
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
          {t('gift.title', { name: key.senderName })}
        </h1>
        <p className="mt-2 text-muted-foreground">{t('gift.subtitle')}</p>
      </div>

      {/* Gift card */}
      <Card className="p-8 sm:p-10 mb-8 border-primary/30 bg-primary/5 text-center">
        <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">
          {'Kaitu VPN'}
        </p>
        <div className="text-5xl sm:text-6xl font-mono font-bold text-primary mb-4">
          {discountDisplay}
        </div>
        <div className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
          <Clock className="w-4 h-4" />
          <span>{t('gift.expires', { days: daysRemaining })}</span>
        </div>
      </Card>

      {/* CTA */}
      <div className="text-center">
        <Button asChild size="lg" className="w-full sm:w-auto px-12 py-6 text-lg font-bold">
          <Link href={ctaHref}>{t('gift.cta')}</Link>
        </Button>
      </div>
    </div>
  );
}
