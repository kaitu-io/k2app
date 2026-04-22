"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import { useAuth } from "@/contexts/AuthContext";
import EmailLogin from "@/components/EmailLogin";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useAppConfig } from "@/contexts/AppConfigContext";
import { UserIcon, GiftIcon } from "lucide-react";

// Cookie helper function
function getCookie(name: string): string | null {
  if (typeof document === 'undefined') return null;
  const nameEQ = name + "=";
  const ca = document.cookie.split(';');
  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];
    while (c.charAt(0) === ' ') c = c.substring(1, c.length);
    if (c.indexOf(nameEQ) === 0) return c.substring(nameEQ.length, c.length);
  }
  return null;
}

export interface PurchaseStep1Props {
  onLoginSuccess?: () => void;
}

/**
 * Step 1 — email binding for unauthenticated users.
 *
 * Member/proxy-target selection was removed when the backend dropped
 * proxy-purchase support (Task 5, error 422002). Each order is now always
 * for the buyer themselves, so authenticated users have nothing to do here
 * and the entire card is hidden.
 */
export default function PurchaseStep1({
  onLoginSuccess
}: PurchaseStep1Props = {}) {
  const { appConfig } = useAppConfig();
  const t = useTranslations();
  const { isAuthenticated } = useAuth();

  // Check for invite code cookie
  const [inviteCodeFromCookie, setInviteCodeFromCookie] = useState<string | null>(null);

  useEffect(() => {
    const code = getCookie('kaitu_invite_code');
    if (code) {
      setInviteCodeFromCookie(code);
    }
  }, []);

  // Authenticated users skip Step 1 entirely.
  if (isAuthenticated) {
    return null;
  }

  return (
    <Card>
      <CardHeader className="pb-0">
        <CardTitle className="flex items-center gap-3 sm:gap-2">
          <div className="flex items-center justify-center w-10 h-10 sm:w-8 sm:h-8 bg-primary text-primary-foreground rounded-full text-base sm:text-sm font-bold flex-shrink-0">
            {t('common.step1')}
          </div>
          <div className="flex items-center gap-2 sm:gap-2">
            <UserIcon className="w-6 h-6 sm:w-5 sm:h-5 text-primary flex-shrink-0" />
            <span className="text-lg sm:text-base font-bold sm:font-semibold leading-tight text-foreground">
              {t('purchase.purchase.bindEmail')}
            </span>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pt-0 px-3 sm:px-6">
        {/* Invite Reward Prompt - Show when user came from invite link */}
        {inviteCodeFromCookie && appConfig?.inviteReward?.purchaseRewardDays && (
          <div className="p-4 bg-gradient-to-r from-amber-50 to-orange-50 dark:from-amber-950/30 dark:to-orange-950/30 border border-amber-200 dark:border-amber-800 rounded-lg mb-4">
            <div className="flex items-start gap-3">
              <GiftIcon className="w-6 h-6 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
              <div>
                <p className="text-base font-bold text-amber-800 dark:text-amber-200">
                  {t('purchase.purchase.inviteRewardTitle')}
                </p>
                <p className="text-sm text-amber-700 dark:text-amber-300 mt-1">
                  {t('purchase.purchase.inviteRewardDesc', { days: appConfig.inviteReward.purchaseRewardDays })}
                </p>
              </div>
            </div>
          </div>
        )}
        <EmailLogin onLoginSuccess={onLoginSuccess} mode="bind" />
      </CardContent>
    </Card>
  );
}
