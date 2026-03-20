"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { useRouter } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import {
  Gift,
  CheckCircle,
  Loader2,
  AlertCircle,
  Sparkles,
  ArrowRight
} from 'lucide-react';
import { api, ApiError, InviteCode, AppConfig } from '@/lib/api';
import { getApiErrorMessage } from '@/lib/api-errors';

function setCookie(name: string, value: string, days: number = 7): void {
  const expires = new Date();
  expires.setTime(expires.getTime() + days * 24 * 60 * 60 * 1000);
  document.cookie = `${name}=${value};expires=${expires.toUTCString()};path=/`;
}

const COUNTDOWN_SECONDS = 10;

interface InviteClientProps {
  code: string;
}

export default function InviteClient({ code }: InviteClientProps) {
  const t = useTranslations();
  const router = useRouter();

  const [inviteInfo, setInviteInfo] = useState<InviteCode | null>(null);
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [countdown, setCountdown] = useState(COUNTDOWN_SECONDS);

  useEffect(() => {
    async function fetchData() {
      try {
        const [inviteData, configData] = await Promise.all([
          api.getInviteCodeInfo(code, { autoRedirectToAuth: false }),
          api.getAppConfig({ autoRedirectToAuth: false })
        ]);
        setInviteInfo(inviteData);
        setAppConfig(configData);
      } catch (err) {
        console.error('Failed to fetch data:', err);
        if (err instanceof ApiError) {
          setError(getApiErrorMessage(err.code, t, t('invite.inviteLanding.invalidCode')));
        } else {
          setError(t('invite.inviteLanding.invalidCode'));
        }
      } finally {
        setLoading(false);
      }
    }

    if (code) {
      fetchData();
    }
  }, [code, t]);

  const handleActivateNow = useCallback(() => {
    if (inviteInfo?.code) {
      setCookie('kaitu_invite_code', inviteInfo.code.toUpperCase(), 30);
    }
    router.push('/purchase');
  }, [inviteInfo, router]);

  // Countdown timer — starts after data loads successfully
  useEffect(() => {
    if (loading || error || !inviteInfo) return;

    if (countdown <= 0) {
      handleActivateNow();
      return;
    }

    const timer = setTimeout(() => setCountdown(c => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [countdown, loading, error, inviteInfo, handleActivateNow]);

  if (loading) {
    return (
      <div className="py-20 text-center">
        <Loader2 className="w-12 h-12 animate-spin mx-auto text-primary" />
        <p className="mt-4 text-muted-foreground">{t('invite.inviteLanding.loading')}</p>
      </div>
    );
  }

  if (error || !inviteInfo || !appConfig) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-20">
        <Card className="p-8 text-center border-destructive/50 bg-destructive/10">
          <AlertCircle className="w-16 h-16 text-destructive mx-auto mb-4" />
          <h2 className="text-2xl font-bold text-foreground mb-2">
            {error || t('invite.inviteLanding.invalidCode')}
          </h2>
          <p className="text-muted-foreground mb-6">
            {t('invite.inviteLanding.invalidCodeDesc')}
          </p>
          <Button
            onClick={() => window.location.href = '/'}
            variant="outline"
          >
            {t('invite.inviteLanding.backToHome')}
          </Button>
        </Card>
      </div>
    );
  }

  const rewardDays = appConfig.inviteReward.purchaseRewardDays;

  // SVG circular progress for countdown
  const radius = 36;
  const circumference = 2 * Math.PI * radius;
  const progress = (countdown / COUNTDOWN_SECONDS) * circumference;

  return (
    <div className="max-w-2xl mx-auto px-4 sm:px-6 lg:px-8 py-16 sm:py-24">
      {/* Hero */}
      <div className="text-center mb-12">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>

        <h1 className="text-3xl sm:text-4xl font-mono font-bold text-foreground mb-3">
          {t('invite.inviteLanding.friendGift')}
        </h1>

        <p className="text-lg text-muted-foreground">
          {t('invite.inviteLanding.friendInvite')}
        </p>
      </div>

      {/* 30-Day Gift — Hero Card */}
      <Card className="p-8 sm:p-10 mb-8 border-primary/20 bg-primary/5">
        <div className="text-center">
          <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">
            {t('invite.inviteLanding.purchaseReward')}
          </p>
          <div className="flex items-baseline justify-center gap-2 mb-4">
            <span className="text-7xl sm:text-8xl font-mono font-bold text-primary">
              {rewardDays}
            </span>
            <span className="text-2xl sm:text-3xl font-mono text-primary/70">
              {t('invite.inviteLanding.days')}
            </span>
          </div>
          <p className="text-lg text-foreground font-medium mb-2">
            {t('invite.inviteLanding.membershipDuration')}
          </p>
          <p className="text-sm text-muted-foreground">
            {t('invite.inviteLanding.exclusiveBonus')}
          </p>
        </div>
      </Card>

      {/* CTA + Countdown */}
      <div className="text-center mb-8">
        <Button
          onClick={handleActivateNow}
          size="lg"
          className="bg-primary text-primary-foreground hover:bg-primary/90 font-mono font-bold px-10 py-6 text-lg mb-6"
        >
          <Sparkles className="w-5 h-5 mr-2" />
          {t('invite.inviteLanding.activateNow')}
          <ArrowRight className="w-5 h-5 ml-2" />
        </Button>

        {/* Countdown */}
        <div className="flex flex-col items-center gap-3">
          <div className="relative w-20 h-20">
            <svg className="w-20 h-20 -rotate-90" viewBox="0 0 80 80">
              <circle
                cx="40" cy="40" r={radius}
                stroke="currentColor"
                strokeWidth="3"
                fill="none"
                className="text-muted/50"
              />
              <circle
                cx="40" cy="40" r={radius}
                stroke="currentColor"
                strokeWidth="3"
                fill="none"
                strokeDasharray={circumference}
                strokeDashoffset={circumference - progress}
                strokeLinecap="round"
                className="text-primary transition-all duration-1000 ease-linear"
              />
            </svg>
            <span className="absolute inset-0 flex items-center justify-center text-xl font-mono font-bold text-foreground">
              {countdown}
            </span>
          </div>
          <p className="text-sm text-muted-foreground">
            {t('invite.inviteLanding.autoRedirect', { seconds: countdown })}
          </p>
        </div>
      </div>

      {/* Invite Code */}
      <div className="text-center mb-8">
        <p className="text-sm text-muted-foreground mb-2">
          {t('invite.inviteLanding.inviteCodeLabel')}
        </p>
        <code className="text-2xl sm:text-3xl font-mono font-bold text-secondary tracking-[0.2em]">
          {inviteInfo.code.toUpperCase()}
        </code>
      </div>

      {/* Benefits */}
      <Card className="p-6 border-border bg-card">
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <CheckCircle className="w-5 h-5 text-primary flex-shrink-0" />
            <span className="text-foreground">
              {t('invite.inviteLanding.bonusHighlight')}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <CheckCircle className="w-5 h-5 text-primary flex-shrink-0" />
            <span className="text-foreground">
              {t('invite.inviteLanding.allPlatforms')}
            </span>
          </div>
        </div>
      </Card>
    </div>
  );
}
