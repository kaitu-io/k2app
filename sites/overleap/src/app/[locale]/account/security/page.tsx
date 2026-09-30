'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { useAuth } from '@/contexts/AuthContext';
import { profileEmail } from '@/lib/api';
import { Button } from '@/components/ui/button';
import ChangePasswordDialog from '@/components/ChangePasswordDialog';

export default function SecurityPage() {
  const t = useTranslations('account');
  const { profile, refresh } = useAuth();
  const [open, setOpen] = useState(false);
  if (!profile) return null;
  const hasPassword = profile.hasPassword;

  return (
    <section className="rounded-xl border bg-card p-6">
      <h2 className="text-lg font-semibold">
        {hasPassword ? t('password.changePassword') : t('password.setPassword')}
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {hasPassword ? t('security.passwordChange') : t('security.passwordSet')}
      </p>
      <Button className="mt-5" onClick={() => setOpen(true)}>
        {hasPassword ? t('password.changePassword') : t('password.setPassword')}
      </Button>
      <ChangePasswordDialog
        open={open}
        onOpenChange={setOpen}
        hasPassword={hasPassword}
        userEmail={profileEmail(profile)}
        onSuccess={() => void refresh()}
      />
    </section>
  );
}
