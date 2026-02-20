"use client";

import { useState, FormEvent, useEffect, Suspense } from "react";
import { useRouter, Link } from "@/i18n/routing";
import { useSearchParams } from "next/navigation";
import { useAuth } from "@/contexts/AuthContext";
import { useTranslations } from "next-intl";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import Header from "@/components/Header";
import Footer from "@/components/Footer";
import Image from "next/image";

function LoginPageContent() {
  const { login, isAuthenticated } = useAuth();
  const t = useTranslations();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [step, setStep] = useState(1); // 1 for email, 2 for code (with optional invite)
  const [isLoading, setIsLoading] = useState(false);
  const [isActivated, setIsActivated] = useState(true); // 用户是否已激活

  // Get the next URL from query params, default to account page
  const next = searchParams.get("next") || "/account";

  useEffect(() => {
    if (isAuthenticated) {
      router.replace(next);
    }
  }, [isAuthenticated, router, next]);

  const handleSendCode = async () => {
    if (!email) {
      toast.error(t('auth.login.enterEmail'));
      return;
    }
    setIsLoading(true);
    try {
      // Get current locale for language preference
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
        console.log(`User activation status: ${response.isActivated}`);
      }

      toast.success(t('auth.login.codeSuccess'));
      setStep(2);
    } catch (error) {
      // Error is handled by api wrapper
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

      // 修复双重 locale 问题：如果 next 已包含 locale，直接 replace
      if (next.startsWith('/')) {
        router.replace(next);
      } else {
        router.push(next);
      }
    } catch (error) {
      // Show error message to user
      if (error instanceof Error) {
        toast.error(error.message);
      } else {
        toast.error(t('auth.login.loginFailed'));
      }
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex flex-col bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />
      
      <div className="flex-1 flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8">
        <div className="w-full max-w-md p-8 space-y-6 bg-white rounded-lg shadow-md dark:bg-gray-800">
          <div className="text-center mb-6">
            <div className="flex justify-center mb-4">
              <Image 
                src="/kaitu-icon.png" 
                alt="Kaitu Logo" 
                width={48}
                height={48}
                className="rounded-lg"
              />
            </div>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">{t('auth.login.title')}</h1>
            <p className="text-gray-600 dark:text-gray-400 mt-2">{t('auth.login.tagline')}</p>
          </div>
        {step === 1 ? (
          <div className="space-y-6">
            <div>
              <Label htmlFor="email">{t('auth.login.email')}</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                placeholder={t('auth.login.emailPlaceholder')}
              />
            </div>
            <Button
              onClick={handleSendCode}
              disabled={isLoading}
              className="w-full"
            >
              {isLoading ? t('auth.login.sendingCode') : t('auth.login.sendCode')}
            </Button>
          </div>
        ) : (
          <form onSubmit={handleLogin} className="space-y-6">
            {/* 如果用户未激活，显示提示信息 */}
            {!isActivated && (
              <div className="p-3 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                <p className="text-sm text-blue-700 dark:text-blue-300">
                  {t('auth.login.inviteCodePrompt')}
                </p>
              </div>
            )}

            <div>
              <Label htmlFor="email">{t('auth.login.email')}</Label>
              <Input
                id="email"
                type="email"
                value={email}
                readOnly
                className="cursor-not-allowed bg-gray-100 dark:bg-gray-700"
              />
            </div>

            <div>
              <Label htmlFor="code">{t('auth.login.verificationCode')}</Label>
              <Input
                id="code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
                placeholder={t('auth.login.codePlaceholder')}
              />
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {t('auth.login.checkSpamFolder')}
              </p>
            </div>

            {/* 如果用户未激活，显示邀请码输入框 */}
            {!isActivated && (
              <div>
                <Label htmlFor="inviteCode">{t('auth.login.inviteCode')}</Label>
                <Input
                  id="inviteCode"
                  value={inviteCode}
                  onChange={(e) => setInviteCode(e.target.value)}
                  placeholder={t('auth.login.inviteCodePlaceholder')}
                  className="uppercase"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  {t('auth.login.inviteCodeOptional')}
                </p>
              </div>
            )}

            <Button type="submit" disabled={isLoading} className="w-full">
              {isLoading ? t('auth.login.loggingIn') : t('auth.login.loginButton')}
            </Button>

            <div className="text-center -mt-2">
              <Button
                type="button"
                variant="link"
                onClick={() => setStep(1)}
                className="text-muted-foreground font-normal"
              >
                {t('auth.login.changeEmail')}
              </Button>
            </div>
          </form>
        )}

        {/* Registration prompt for new users */}
        <div className="pt-6 mt-6 border-t border-gray-200 dark:border-gray-700">
          <div className="text-center">
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
              {t('auth.login.noAccountYet')}
            </p>
            <Link href="/purchase">
              <Button
                variant="outline"
                className="w-full border-blue-500 text-blue-600 hover:bg-blue-50 dark:border-blue-400 dark:text-blue-400 dark:hover:bg-blue-950/20"
              >
                {t('auth.login.startService')}
              </Button>
            </Link>
          </div>
        </div>
        </div>
      </div>
      
      <Footer />
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={
      <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 flex items-center justify-center">
        <div className="text-center">
          <div className="w-8 h-8 border-4 border-blue-600 border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
          <p className="text-gray-600 dark:text-gray-300">{"Loading..."}</p>
        </div>
      </div>
    }>
      <LoginPageContent />
    </Suspense>
  );
}
