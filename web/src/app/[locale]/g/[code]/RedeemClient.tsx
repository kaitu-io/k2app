'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Gift, AlertCircle, Clock, CheckCircle, Loader2 } from 'lucide-react';
import type { LicenseKeyPublic } from '@/lib/api';
import { api, ApiError } from '@/lib/api';

const ErrorLicenseKeyNotFound = 400007;
const ErrorLicenseKeyUsed = 400008;
const ErrorLicenseKeyExpired = 400009;
const ErrorLicenseKeyNotMatch = 400010;
const ErrorLicenseKeyAlreadyRedeemed = 400011;

function getDaysRemaining(expiresAt: number): number {
  const now = Math.floor(Date.now() / 1000);
  const diff = expiresAt - now;
  return Math.max(0, Math.ceil(diff / 86400));
}

export default function RedeemClient({ code }: { code: string }) {
  const t = useTranslations('licenseKeys');
  const [key, setKey] = useState<LicenseKeyPublic | null>(null);
  const [fetchState, setFetchState] = useState<'loading' | 'ready' | 'notFound'>('loading');
  const [redeemState, setRedeemState] = useState<'idle' | 'loading' | 'success' | 'error'>('idle');
  const [redeemDays, setRedeemDays] = useState<number>(0);
  const [errorKey, setErrorKey] = useState<string>('');

  useEffect(() => {
    api.getLicenseKey(code)
      .then((data) => {
        setKey(data);
        setFetchState('ready');
      })
      .catch(() => {
        setFetchState('notFound');
      });
  }, [code]);

  // Loading state
  if (fetchState === 'loading') {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Loader2 className="w-10 h-10 text-muted-foreground mx-auto animate-spin" />
      </div>
    );
  }

  // Not found state
  if (fetchState === 'notFound') {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('landing.notFound')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.subtitle')}</p>
          <Button asChild variant="outline" size="lg">
            <Link href="/g">{t('landing.title')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Used state
  if (key?.isUsed) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.used')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.subtitle')}</p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Expired state
  if (key?.isExpired) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <Clock className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.expired')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.subtitle')}</p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Success state
  if (redeemState === 'success') {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-primary/30 bg-primary/5">
          <CheckCircle className="w-14 h-14 text-primary mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.successTitle')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.successBody', { days: redeemDays })}</p>
          <Button asChild size="lg">
            <Link href="/account">{t('gift.viewAccount')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  if (!key) return null;

  const daysRemaining = getDaysRemaining(key.expiresAt);

  const handleRedeem = async () => {
    setRedeemState('loading');
    setErrorKey('');
    try {
      const result = await api.redeemLicenseKey(code);
      setRedeemDays(result.planDays);
      setRedeemState('success');
    } catch (err) {
      if (err instanceof ApiError) {
        const errCode = err.code as number;
        switch (errCode) {
          case ErrorLicenseKeyUsed:
            setErrorKey('gift.used');
            break;
          case ErrorLicenseKeyExpired:
            setErrorKey('gift.expired');
            break;
          case ErrorLicenseKeyNotFound:
          case ErrorLicenseKeyNotMatch:
            setErrorKey('gift.notEligible');
            break;
          case ErrorLicenseKeyAlreadyRedeemed:
            setErrorKey('gift.alreadyRedeemed');
            break;
          default:
            setErrorKey('gift.redeemFailed');
        }
      } else {
        setErrorKey('gift.redeemFailed');
      }
      setRedeemState('error');
    }
  };

  return (
    <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
          {key.senderName ? t('gift.title', { name: key.senderName }) : t('gift.titleAnonymous')}
        </h1>
        <p className="mt-2 text-muted-foreground">{t('gift.subtitle')}</p>
      </div>

      <Card className="p-8 sm:p-10 mb-8 border-primary/30 bg-primary/5 text-center">
        <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">Kaitu VPN</p>
        <div className="text-5xl sm:text-6xl font-mono font-bold text-primary mb-4">
          {t('gift.planDays', { days: key.planDays })}
        </div>
        <div className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
          <Clock className="w-4 h-4" />
          <span>{t('gift.expires', { days: daysRemaining })}</span>
        </div>
      </Card>

      {redeemState === 'error' && errorKey && (
        <p className="text-sm text-destructive text-center mb-4">
          {t(errorKey as Parameters<typeof t>[0])}
        </p>
      )}

      <div className="text-center">
        <Button
          size="lg"
          className="w-full sm:w-auto px-12 py-6 text-lg font-bold"
          onClick={handleRedeem}
          disabled={redeemState === 'loading'}
        >
          {redeemState === 'loading' ? t('gift.loading') : t('gift.cta')}
        </Button>
      </div>
    </div>
  );
}
