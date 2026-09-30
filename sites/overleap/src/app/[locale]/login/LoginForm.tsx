'use client';

import { useEffect, useState, type FormEvent } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { useSearchParams } from 'next/navigation';
import { toast } from 'sonner';
import { useRouter } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { api, ApiError, ErrorCode } from '@/lib/api';
import { errorMessage } from '@/lib/api-errors';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { safeNext } from '@/lib/safe-next';

type Mode = 'code' | 'password';

export default function LoginForm() {
  const t = useTranslations('auth');
  const tErr = useTranslations('errors');
  const locale = useLocale();
  const router = useRouter();
  const next = safeNext(useSearchParams().get('next'));
  const { profile, refresh } = useAuth();

  const [mode, setMode] = useState<Mode>('code');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [codeSent, setCodeSent] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (profile) router.replace(next);
  }, [profile, router, next]);

  async function finish(accessToken: string) {
    await api.applyLoginCredentials(accessToken);
    await refresh(); // the effect above redirects once the profile arrives
  }

  async function sendCode(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api.sendCode(email.trim(), locale);
      setCodeSent(true);
      toast.success(t('codeSent', { email: email.trim() }));
    } catch (err) {
      toast.error(errorMessage(err, tErr));
    } finally {
      setBusy(false);
    }
  }

  async function verifyCode(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await api.webLogin(email.trim(), code.trim(), locale);
      await finish(res.accessToken);
    } catch (err) {
      toast.error(errorMessage(err, tErr));
      if (err instanceof ApiError && err.code === ErrorCode.VerificationCodeExpired) {
        setCode('');
        setCodeSent(false);
      }
    } finally {
      setBusy(false);
    }
  }

  async function loginWithPassword(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await api.passwordLogin(email.trim(), password, locale);
      await finish(res.accessToken);
    } catch (err) {
      toast.error(errorMessage(err, tErr));
    } finally {
      setBusy(false);
    }
  }

  const emailField = (
    <div className="space-y-2">
      <Label htmlFor="email">{t('email')}</Label>
      <Input
        id="email"
        type="email"
        autoComplete="email"
        required
        value={email}
        readOnly={mode === 'code' && codeSent}
        onChange={(e) => setEmail(e.target.value)}
        placeholder={t('emailPlaceholder')}
      />
    </div>
  );

  return (
    <div>
      <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
      <p className="mt-2 text-muted-foreground">{t('subtitle')}</p>

      <div className="mt-8 rounded-xl border bg-card p-6">
        {mode === 'code' && !codeSent && (
          <form onSubmit={sendCode} className="space-y-5">
            {emailField}
            <Button type="submit" className="w-full" disabled={busy}>
              {busy ? t('sending') : t('sendCode')}
            </Button>
          </form>
        )}

        {mode === 'code' && codeSent && (
          <form onSubmit={verifyCode} className="space-y-5">
            {emailField}
            <div className="space-y-2">
              <Label htmlFor="code">{t('code')}</Label>
              <Input
                id="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                required
                autoFocus
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={t('codePlaceholder')}
              />
              <p className="text-xs text-muted-foreground">{t('checkSpam')}</p>
            </div>
            <Button type="submit" className="w-full" disabled={busy}>
              {busy ? t('signingIn') : t('signIn')}
            </Button>
            <Button type="button" variant="link" className="w-full text-muted-foreground" onClick={() => setCodeSent(false)}>
              {t('changeEmail')}
            </Button>
          </form>
        )}

        {mode === 'password' && (
          <form onSubmit={loginWithPassword} className="space-y-5">
            {emailField}
            <div className="space-y-2">
              <Label htmlFor="password">{t('password')}</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <Button type="submit" className="w-full" disabled={busy}>
              {busy ? t('signingIn') : t('signIn')}
            </Button>
          </form>
        )}

        <div className="mt-4 border-t pt-4 text-center">
          <Button
            type="button"
            variant="link"
            className="text-muted-foreground"
            onClick={() => {
              setMode(mode === 'code' ? 'password' : 'code');
              setCodeSent(false);
            }}
          >
            {mode === 'code' ? t('usePassword') : t('useCode')}
          </Button>
        </div>
      </div>

      <p className="mt-6 text-center text-sm text-muted-foreground">{t('newAccountNote')}</p>
    </div>
  );
}
