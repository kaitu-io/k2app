'use client';

export const dynamic = "force-dynamic";

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { useAuth } from '@/contexts/AuthContext';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Lock } from 'lucide-react';
import ChangePasswordDialog from '@/components/ChangePasswordDialog';

export default function SecurityPage() {
  const t = useTranslations('admin.account');
  const { user } = useAuth();
  const [dialogOpen, setDialogOpen] = useState(false);

  if (!user) return null;
  const hasPassword = user.hasPassword ?? false;

  return (
    <div className="container max-w-3xl py-6">
      <h1 className="text-2xl font-semibold mb-2">{t('security.title')}</h1>
      <p className="text-muted-foreground mb-6">{t('security.description')}</p>

      <Card className="p-6">
        <div className="flex items-start gap-4">
          <Lock className="w-5 h-5 mt-0.5 text-muted-foreground" />
          <div className="flex-1">
            <h2 className="font-medium mb-1">
              {hasPassword ? t('password.changePassword') : t('password.setPassword')}
            </h2>
            <p className="text-sm text-muted-foreground mb-4">
              {hasPassword ? t('security.passwordChange') : t('security.passwordSet')}
            </p>
            <Button onClick={() => setDialogOpen(true)}>
              {hasPassword ? t('password.changePassword') : t('password.setPassword')}
            </Button>
          </div>
        </div>
      </Card>

      <ChangePasswordDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        hasPassword={hasPassword}
        userEmail={user.email || ''}
      />
    </div>
  );
}
