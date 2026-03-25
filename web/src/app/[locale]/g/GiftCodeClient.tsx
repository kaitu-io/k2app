'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Gift, AlertCircle, Clock, CheckCircle, Loader2, Search } from 'lucide-react';
import type { LicenseKeyPublic } from '@/lib/api';
import { api, ApiError } from '@/lib/api';

const ErrorLicenseKeyNotFound = 400007;
const ErrorLicenseKeyUsed = 400008;
const ErrorLicenseKeyExpired = 400009;
const ErrorLicenseKeyNotMatch = 400010;
const ErrorLicenseKeyAlreadyRedeemed = 400011;

type PageState = 'idle' | 'looking' | 'found' | 'redeeming' | 'success' | 'error';

function getDaysRemaining(expiresAt: number): number {
  const now = Math.floor(Date.now() / 1000);
  return Math.max(0, Math.ceil((expiresAt - now) / 86400));
}

export default function GiftCodeClient() {
  const t = useTranslations('licenseKeys');
  const [inputCode, setInputCode] = useState('');
  const [state, setState] = useState<PageState>('idle');
  const [keyData, setKeyData] = useState<LicenseKeyPublic | null>(null);
  const [redeemDays, setRedeemDays] = useState(0);
  const [errorMsg, setErrorMsg] = useState('');

  const handleLookup = async () => {
    const code = inputCode.trim().toUpperCase();
    if (!code) return;
    setState('looking');
    setErrorMsg('');
    try {
      const data = await api.getLicenseKey(code);
      setKeyData(data);
      setState('found');
    } catch {
      setErrorMsg(t('landing.notFound'));
      setState('error');
    }
  };

  const handleRedeem = async () => {
    const code = inputCode.trim().toUpperCase();
    setState('redeeming');
    setErrorMsg('');
    try {
      const result = await api.redeemLicenseKey(code);
      setRedeemDays(result.planDays);
      setState('success');
    } catch (err) {
      if (err instanceof ApiError) {
        const errCode = err.code as number;
        switch (errCode) {
          case ErrorLicenseKeyUsed:
            setErrorMsg(t('gift.used'));
            break;
          case ErrorLicenseKeyExpired:
            setErrorMsg(t('gift.expired'));
            break;
          case ErrorLicenseKeyNotFound:
          case ErrorLicenseKeyNotMatch:
            setErrorMsg(t('gift.notEligible'));
            break;
          case ErrorLicenseKeyAlreadyRedeemed:
            setErrorMsg(t('gift.alreadyRedeemed'));
            break;
          default:
            setErrorMsg(t('gift.redeemFailed'));
        }
      } else {
        setErrorMsg(t('gift.redeemFailed'));
      }
      setState('error');
    }
  };

  const handleReset = () => {
    setInputCode('');
    setKeyData(null);
    setErrorMsg('');
    setState('idle');
  };

  // Success state
  if (state === 'success') {
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

  // Found state — show key info + redeem button
  if ((state === 'found' || state === 'redeeming') && keyData) {
    const daysRemaining = getDaysRemaining(keyData.expiresAt);

    if (keyData.isUsed) {
      return (
        <div className="max-w-lg mx-auto px-4 py-20 text-center">
          <Card className="p-10 border-muted bg-muted/20">
            <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.used')}</h1>
            <Button asChild variant="outline" size="lg" className="mt-4">
              <Link href="/purchase">{t('gift.fallback')}</Link>
            </Button>
          </Card>
        </div>
      );
    }
    if (keyData.isExpired) {
      return (
        <div className="max-w-lg mx-auto px-4 py-20 text-center">
          <Card className="p-10 border-muted bg-muted/20">
            <Clock className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.expired')}</h1>
            <Button asChild variant="outline" size="lg" className="mt-4">
              <Link href="/purchase">{t('gift.fallback')}</Link>
            </Button>
          </Card>
        </div>
      );
    }

    return (
      <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
        <div className="text-center mb-10">
          <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
            <Gift className="w-10 h-10 text-primary" />
          </div>
          <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
            {keyData.senderName
              ? t('gift.title', { name: keyData.senderName })
              : t('gift.titleAnonymous')}
          </h1>
          <p className="mt-2 text-muted-foreground">{t('gift.subtitle')}</p>
        </div>

        <Card className="p-8 sm:p-10 mb-8 border-primary/30 bg-primary/5 text-center">
          <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">Kaitu VPN</p>
          <div className="text-5xl sm:text-6xl font-mono font-bold text-primary mb-4">
            {t('gift.planDays', { days: keyData.planDays })}
          </div>
          <div className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
            <Clock className="w-4 h-4" />
            <span>{t('gift.expires', { days: daysRemaining })}</span>
          </div>
        </Card>

        {errorMsg && (
          <p className="text-sm text-destructive text-center mb-4">{errorMsg}</p>
        )}

        <div className="flex flex-col items-center gap-3">
          <Button
            size="lg"
            className="w-full sm:w-auto px-12 py-6 text-lg font-bold"
            onClick={handleRedeem}
            disabled={state === 'redeeming'}
          >
            {state === 'redeeming' ? t('gift.loading') : t('gift.cta')}
          </Button>
          <button
            onClick={handleReset}
            className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground"
          >
            {t('landing.reenter')}
          </button>
        </div>
      </div>
    );
  }

  // Idle / Error state — show input form
  return (
    <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
          {t('landing.title')}
        </h1>
        <p className="mt-2 text-muted-foreground">{t('landing.subtitle')}</p>
      </div>

      <Card className="p-8 sm:p-10">
        <div className="flex gap-3">
          <Input
            value={inputCode}
            onChange={(e) => setInputCode(e.target.value.toUpperCase())}
            placeholder={t('landing.placeholder')}
            maxLength={8}
            className="font-mono text-lg tracking-widest uppercase text-center"
            onKeyDown={(e) => e.key === 'Enter' && handleLookup()}
          />
          <Button
            onClick={handleLookup}
            disabled={!inputCode.trim() || state === 'looking'}
            size="lg"
          >
            {state === 'looking' ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Search className="w-4 h-4" />
            )}
          </Button>
        </div>

        {errorMsg && (
          <p className="text-sm text-destructive text-center mt-4">{errorMsg}</p>
        )}
      </Card>
    </div>
  );
}
