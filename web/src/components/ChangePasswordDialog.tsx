'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from 'sonner';
import { api, ApiError } from '@/lib/api';
import { getApiErrorMessage } from '@/lib/api-errors';
import { checkPasswordStrength, PASSWORD_MIN_LENGTH } from '@/lib/password-strength';
import PasswordStrengthMeter from './PasswordStrengthMeter';
import { Loader2 } from 'lucide-react';

interface ChangePasswordDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hasPassword: boolean;
  userEmail: string;
  onSuccess?: () => void;
}

export default function ChangePasswordDialog({
  open,
  onOpenChange,
  hasPassword,
  userEmail,
  onSuccess,
}: ChangePasswordDialogProps) {
  const t = useTranslations();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [strength, setStrength] = useState<{
    score: 0 | 1 | 2 | 3 | 4;
    tooShort: boolean;
    isValid: boolean;
  }>({
    score: 0,
    tooShort: true,
    isValid: false,
  });

  useEffect(() => {
    let cancelled = false;
    if (!password) {
      setStrength({ score: 0, tooShort: true, isValid: false });
      return;
    }
    checkPasswordStrength(password, userEmail ? [userEmail] : []).then((r) => {
      if (!cancelled) setStrength(r);
    });
    return () => {
      cancelled = true;
    };
  }, [password, userEmail]);

  const canSubmit =
    !!password && !!confirm && strength.isValid && password === confirm && !submitting;

  const close = () => {
    if (submitting) return;
    setPassword('');
    setConfirm('');
    setStrength({ score: 0, tooShort: true, isValid: false });
    onOpenChange(false);
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await api.setPassword({ password, confirmPassword: confirm });
      toast.success(t('admin.account.password.setSuccess'));
      onSuccess?.();
      close();
    } catch (error) {
      if (error instanceof ApiError)
        toast.error(getApiErrorMessage(error.code, t, error.message));
      else toast.error(t('admin.account.password.setFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const title = hasPassword
    ? t('admin.account.password.changePassword')
    : t('admin.account.password.setPassword');

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div>
            <Label htmlFor="change-pw-new">
              {t('admin.account.password.newPassword')}
            </Label>
            <Input
              id="change-pw-new"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              className="mt-1"
            />
            <p className="text-xs text-muted-foreground mt-1">
              {t('admin.account.password.requirements', { length: PASSWORD_MIN_LENGTH })}
            </p>
            {password && (
              <PasswordStrengthMeter score={strength.score} tooShort={strength.tooShort} />
            )}
          </div>
          <div>
            <Label htmlFor="change-pw-confirm">
              {t('admin.account.password.confirmPassword')}
            </Label>
            <Input
              id="change-pw-confirm"
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              className="mt-1"
            />
            {confirm && password !== confirm && (
              <p className="text-xs text-red-500 mt-1">
                {t('admin.account.password.mismatch')}
              </p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={close} disabled={submitting}>
            {t('common.common.cancel')}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t('common.common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
