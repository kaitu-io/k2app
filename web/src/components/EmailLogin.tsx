"use client";

import { useState, FormEvent, useEffect } from "react";
import { useTranslations } from "next-intl";
import { useAuth } from "@/contexts/AuthContext";
import { useAppConfig } from "@/contexts/AppConfigContext";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { Mail, ArrowRight, Check, AlertCircleIcon } from "lucide-react";

// Cookie helper functions
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

export interface EmailLoginProps {
  onLoginSuccess?: () => void;
  mode?: 'login' | 'bind';
}

export default function EmailLogin({ onLoginSuccess, mode = 'login' }: EmailLoginProps) {
  const { login } = useAuth();
  const { appConfig } = useAppConfig();
  const t = useTranslations();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [step, setStep] = useState(1); // 1:邮箱 2:验证码（未激活时同时显示邀请码）
  const [isLoading, setIsLoading] = useState(false);
  const [isActivated, setIsActivated] = useState(true); // 用户激活状态
  const [hasInviteCodeCookie, setHasInviteCodeCookie] = useState(false); // 是否有邀请码 cookie

  // Load invite code from cookie on component mount
  useEffect(() => {
    const savedInviteCode = getCookie('kaitu_invite_code');
    if (savedInviteCode) {
      setInviteCode(savedInviteCode);
      setHasInviteCodeCookie(true);
      console.log('[EmailLogin] Auto-filled invite code from cookie:', savedInviteCode);
    }
  }, []);

  const isValidEmail = (email: string) => {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    return emailRegex.test(email);
  };

  const handleSendCode = async () => {
    if (!email) {
      toast.error(t('auth.login.enterEmail'));
      return;
    }
    setIsLoading(true);
    try {
      // Use register-code endpoint to support new user registration
      // Include language preference based on current locale
      const userLanguage = typeof window !== 'undefined' ?
        window.location.pathname.split('/')[1] || 'en-US' : 'en-US';

      const response = await api.sendCode({
        email,
        language: userLanguage
      }, {
        autoRedirectToAuth: false,
      });

      // 保存用户激活状态
      if (response) {
        setIsActivated(response.isActivated);
        console.log(`[EmailLogin] User activation status: ${response.isActivated}`);
      }

      toast.success(t('auth.login.codeSuccess'));
      setStep(2);
    } catch (error) {
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleLogin = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setIsLoading(true);

    try {
      // Get current locale for language preference
      const userLanguage = typeof window !== 'undefined' ?
        window.location.pathname.split('/')[1] || 'en-US' : 'en-US';

      const response = await api.webLogin({
        email,
        verificationCode: code,
        inviteCode: inviteCode.trim() || undefined, // 如果未激活，发送邀请码（可选）
        language: userLanguage,
      }, {
        autoRedirectToAuth: false,
      });

      // 登录成功 - Server already set HttpOnly cookie
      toast.success(t('auth.login.loginSuccess'));
      const { user } = response;
      login(user);
      onLoginSuccess?.();
    } catch (error) {
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="space-y-1">
      {step === 1 ? (
        <div className="space-y-4 sm:space-y-4">
          <div>
            <Label htmlFor="login-email" className="flex items-center gap-2 text-base sm:text-sm font-bold sm:font-medium">
              <Mail className="w-5 h-5 sm:w-4 sm:h-4" />
              {t('auth.login.email')}
            </Label>
            <Input
              id="login-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder={t('auth.login.emailPlaceholder')}
              className="mt-2 sm:mt-1 text-lg sm:text-base py-3 sm:py-2 px-4 sm:px-3"
            />
          </div>
          {isValidEmail(email) && (
            <Button
              onClick={handleSendCode}
              disabled={isLoading}
              className="w-full font-bold text-lg sm:text-base py-6 sm:py-3"
              size="lg"
            >
              {isLoading ? (
                <span className="text-lg sm:text-base">{t('auth.login.sendingCode')}</span>
              ) : (
                <>
                  <span className="text-lg sm:text-base">
                    {mode === 'bind' ? t('purchase.purchase.sendVerificationCode') : t('auth.login.sendCode')}
                  </span>
                  <ArrowRight className="w-5 h-5 sm:w-4 sm:h-4 ml-3 sm:ml-2" />
                </>
              )}
            </Button>
          )}
        </div>
      ) : (
        <form onSubmit={handleLogin} className="space-y-4 sm:space-y-4">
          {/* 如果用户未激活，显示提示信息 */}
          {!isActivated && (
            <div className="p-3 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <p className="text-sm text-blue-700 dark:text-blue-300">
                {t('auth.login.inviteCodePrompt')}
              </p>
            </div>
          )}

          {/* 如果用户已激活但有邀请码 cookie，显示无法获得奖励的提示 */}
          {isActivated && hasInviteCodeCookie && appConfig?.inviteReward?.purchaseRewardDays && (
            <div className="p-3 bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg">
              <div className="flex items-start gap-2">
                <AlertCircleIcon className="w-4 h-4 text-gray-500 dark:text-gray-400 flex-shrink-0 mt-0.5" />
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  {t('auth.login.existingUserNoReward')}
                </p>
              </div>
            </div>
          )}

          <div>
            <Label htmlFor="login-email-readonly" className="flex items-center gap-2 text-base sm:text-sm font-bold sm:font-medium">
              <Mail className="w-5 h-5 sm:w-4 sm:h-4" />
              {t('auth.login.email')}
            </Label>
            <div className="flex items-center gap-2 sm:gap-2 mt-2 sm:mt-1">
              <Input
                id="login-email-readonly"
                type="email"
                value={email}
                readOnly
                className="cursor-not-allowed bg-gray-100 dark:bg-gray-800 text-lg sm:text-base py-3 sm:py-2 px-4 sm:px-3"
              />
              <Check className="w-5 h-5 text-green-500" />
            </div>
          </div>

          <div>
            <Label htmlFor="login-code">
              {t('auth.login.verificationCode')}
            </Label>
            <Input
              id="login-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t('auth.login.codePlaceholder')}
              className="mt-1"
            />
            <p className="text-xs text-muted-foreground mt-1">
              {t('auth.login.checkSpamFolder')}
            </p>
          </div>

          {/* 如果用户未激活，显示邀请码输入框 */}
          {!isActivated && (
            <div>
              <Label htmlFor="login-invitecode">
                {t('auth.login.inviteCode')}
              </Label>
              <Input
                id="login-invitecode"
                value={inviteCode}
                onChange={(e) => setInviteCode(e.target.value)}
                placeholder={t('auth.login.inviteCodePlaceholder')}
                className="mt-1 uppercase"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {t('auth.login.inviteCodeOptional')}
              </p>
            </div>
          )}

          <div className="space-y-2">
            <Button
              type="submit"
              disabled={isLoading || !code}
              className="w-full"
              size="lg"
            >
              {isLoading ? (
                mode === 'bind' ? t('purchase.purchase.bindingEmail') : t('auth.login.loggingIn')
              ) : (
                <>
                  {mode === 'bind' ? t('purchase.purchase.confirmBinding') : t('auth.login.loginButton')}
                  <ArrowRight className="w-4 h-4 ml-2" />
                </>
              )}
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setStep(1)}
              className="w-full text-muted-foreground"
              size="sm"
            >
              {t('auth.login.changeEmail')}
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}