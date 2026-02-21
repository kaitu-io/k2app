"use client";

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useRouter } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS } from '@/lib/constants';
import {
  detectDevice,
  getPrimaryDownloadLink,
  openDownloadInNewTab,
  DeviceInfo
} from '@/lib/device-detection';
import {
  Download,
  Gift,
  CheckCircle,
  Smartphone,
  Monitor,
  Loader2,
  AlertCircle,
  QrCode,
  Sparkles
} from 'lucide-react';
import { api, ApiError, InviteCode, AppConfig } from '@/lib/api';

// Cookie helper function
function setCookie(name: string, value: string, days: number = 7): void {
  const expires = new Date();
  expires.setTime(expires.getTime() + days * 24 * 60 * 60 * 1000);
  document.cookie = `${name}=${value};expires=${expires.toUTCString()};path=/`;
}

interface InviteClientProps {
  /** The invite code from the URL path parameter */
  code: string;
}

/**
 * InviteClient — Client Component for the invite landing page.
 *
 * Handles all browser-dependent and dynamic logic:
 * - API fetch for invite code info + app config
 * - Device detection via navigator.userAgent
 * - Cookie writes (setCookie) for invite code persistence
 * - Router push to /purchase on activation
 * - useState for inviteInfo, appConfig, loading, error, device, primaryLink
 *
 * The server shell (s/[code]/page.tsx) renders static structure with i18n
 * text before this component hydrates.
 */
export default function InviteClient({ code }: InviteClientProps) {
  const t = useTranslations();
  const router = useRouter();

  const [inviteInfo, setInviteInfo] = useState<InviteCode | null>(null);
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [primaryLink, setPrimaryLink] = useState<string | null>(null);

  // Fetch invite code info and app config
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
          setError(err.message || t('invite.inviteLanding.invalidCode'));
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

  // Device detection
  useEffect(() => {
    const deviceInfo = detectDevice();
    const primaryDownloadLink = getPrimaryDownloadLink(DOWNLOAD_LINKS);

    setDevice(deviceInfo);
    setPrimaryLink(primaryDownloadLink);
  }, []);

  const handleDownload = () => {
    if (primaryLink) {
      openDownloadInNewTab(primaryLink);
    }
  };

  const handleActivateNow = () => {
    // Save invite code to cookie
    if (inviteInfo?.code) {
      setCookie('kaitu_invite_code', inviteInfo.code.toUpperCase(), 30);
    }
    // Redirect to purchase page
    router.push('/purchase');
  };

  if (loading) {
    return (
      <div className="py-20 text-center">
        <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-600" />
        <p className="mt-4 text-gray-600 dark:text-gray-300">{t('invite.inviteLanding.loading')}</p>
      </div>
    );
  }

  if (error || !inviteInfo || !appConfig) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-20">
        <Card className="p-8 text-center border-red-200 bg-red-50 dark:bg-red-900/20">
          <AlertCircle className="w-16 h-16 text-red-600 mx-auto mb-4" />
          <h2 className="text-2xl font-bold text-red-900 dark:text-red-100 mb-2">
            {error || t('invite.inviteLanding.invalidCode')}
          </h2>
          <p className="text-red-700 dark:text-red-200 mb-6">
            {t('invite.inviteLanding.invalidCodeDesc')}
          </p>
          <Button onClick={() => window.location.href = '/'} variant="outline">
            {t('invite.inviteLanding.backToHome')}
          </Button>
        </Card>
      </div>
    );
  }

  const rewardDays = appConfig.inviteReward.purchaseRewardDays;
  const platformName = device?.isMobile
    ? (device.type === 'ios' ? 'iPhone/iPad' : 'Android')
    : (device?.type === 'macos' ? 'macOS' : 'Windows');

  return (
    <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
      {/* Hero Section */}
      <div className="text-center mb-12">
        <div className="inline-flex items-center justify-center p-3 bg-gradient-to-br from-pink-100 to-purple-100 dark:from-pink-900/30 dark:to-purple-900/30 rounded-full mb-6">
          <Gift className="w-8 h-8 text-pink-600 dark:text-pink-300" />
        </div>

        <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
          {t('invite.inviteLanding.welcomeTo')} <span className="text-blue-600">{"开途"}</span>
        </h1>

        <p className="text-xl text-gray-600 dark:text-gray-300 mb-2">
          {t('invite.inviteLanding.tagline')}
        </p>

        <p className="text-lg font-medium text-pink-600 dark:text-pink-400">
          {t('invite.inviteLanding.friendInvite')}
        </p>
      </div>

      {/* 友情赠礼卡片 */}
      <Card className="p-8 mb-6 border-2 border-pink-200 bg-gradient-to-r from-pink-50 via-purple-50 to-blue-50 dark:from-pink-900/20 dark:via-purple-900/20 dark:to-blue-900/20 shadow-lg">
        <div className="text-center mb-6">
          <div className="inline-flex items-center justify-center p-4 bg-gradient-to-br from-pink-500 to-purple-600 rounded-full mb-4">
            <Gift className="w-10 h-10 text-white" />
          </div>
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-2">
            {t('invite.inviteLanding.friendGift')}
          </h2>
          <p className="text-base text-gray-600 dark:text-gray-300 max-w-2xl mx-auto">
            {t('invite.inviteLanding.friendGiftDesc')}
          </p>
        </div>
      </Card>

      {/* 奖励详情卡片 */}
      <Card className="p-8 mb-8 border-2 border-blue-200 bg-gradient-to-r from-blue-50 to-purple-50 dark:from-blue-900/20 dark:to-purple-900/20 shadow-lg">
        <div className="flex items-start space-x-4">
          <div className="flex-shrink-0">
            <div className="w-16 h-16 bg-gradient-to-br from-blue-600 to-purple-600 rounded-full flex items-center justify-center">
              <Sparkles className="w-8 h-8 text-white" />
            </div>
          </div>

          <div className="flex-1">
            <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-3">
              {t('invite.inviteLanding.rewardTitle')}
            </h2>

            <div className="space-y-3">
              <div className="flex items-center space-x-3">
                <CheckCircle className="w-5 h-5 text-green-600 flex-shrink-0" />
                <span className="text-lg text-gray-700 dark:text-gray-200">
                  {t('invite.inviteLanding.purchaseReward')} <span className="font-bold text-blue-600 text-xl">{rewardDays} {t('invite.inviteLanding.days')}</span> {t('invite.inviteLanding.membershipDuration')}
                </span>
              </div>

              <div className="flex items-center space-x-3">
                <Sparkles className="w-5 h-5 text-purple-600 flex-shrink-0" />
                <span className="text-lg font-medium text-purple-700 dark:text-purple-300">
                  {t('invite.inviteLanding.exclusiveBonus')}
                </span>
              </div>

              <div className="flex items-center space-x-3">
                <CheckCircle className="w-5 h-5 text-green-600 flex-shrink-0" />
                <span className="text-lg text-gray-700 dark:text-gray-200">
                  {t('invite.inviteLanding.coreOpenSource')}
                </span>
              </div>

              <div className="flex items-center space-x-3">
                <CheckCircle className="w-5 h-5 text-green-600 flex-shrink-0" />
                <span className="text-lg text-gray-700 dark:text-gray-200">
                  {t('invite.inviteLanding.allPlatforms')}
                </span>
              </div>
            </div>

            <div className="mt-4 p-3 bg-orange-100 dark:bg-orange-900/30 rounded-lg border border-orange-300 dark:border-orange-700">
              <p className="text-sm text-orange-800 dark:text-orange-200 text-center font-medium">
                {t('invite.inviteLanding.bonusHighlight')}
              </p>
            </div>
          </div>
        </div>
      </Card>

      {/* 邀请码展示 + 立即开通按钮 */}
      <Card className="p-6 mb-8 bg-white dark:bg-gray-800 border-2 border-purple-200 shadow-lg">
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4">
          <div className="flex items-center space-x-3">
            <QrCode className="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <span className="text-sm text-gray-600 dark:text-gray-400">{t('invite.inviteLanding.inviteCodeLabel')}</span>
            <code className="text-2xl font-mono font-bold text-blue-600 dark:text-blue-400 tracking-wider">
              {inviteInfo.code.toUpperCase()}
            </code>
          </div>

          {/* 立即开通按钮 */}
          <Button
            onClick={handleActivateNow}
            size="lg"
            className="bg-gradient-to-r from-orange-500 to-pink-500 hover:from-orange-600 hover:to-pink-600 text-white font-bold px-8 py-6 text-lg shadow-2xl transform hover:scale-105 transition-all duration-200 whitespace-nowrap"
          >
            <Sparkles className="w-6 h-6 mr-2" />
            {t('invite.inviteLanding.activateNow')}
          </Button>
        </div>
        <p className="text-sm text-center text-gray-500 dark:text-gray-400 mt-4">
          {t('invite.inviteLanding.activateNowDesc')}
        </p>
      </Card>

      {/* 下载引导 */}
      <Card className="p-8 mb-8 border-2 border-purple-200 bg-white dark:bg-gray-800 shadow-lg">
        <div className="text-center">
          <div className="w-20 h-20 mx-auto mb-6 bg-gradient-to-br from-blue-600 to-purple-600 rounded-full flex items-center justify-center">
            {device?.isMobile ? (
              <Smartphone className="w-10 h-10 text-white" />
            ) : (
              <Monitor className="w-10 h-10 text-white" />
            )}
          </div>

          <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-3">
            {t('invite.inviteLanding.downloadTitle')}
          </h3>

          <p className="text-lg text-gray-600 dark:text-gray-300 mb-6">
            {t('invite.inviteLanding.detectedDevice')}<span className="font-semibold text-blue-600">{platformName}</span>
          </p>

          {primaryLink ? (
            <Button
              onClick={handleDownload}
              size="lg"
              className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700 text-white font-bold px-12 py-6 text-lg shadow-xl"
            >
              <Download className="w-6 h-6 mr-3" />
              {t('invite.inviteLanding.downloadFor')} {platformName} {t('invite.inviteLanding.version')}
            </Button>
          ) : (
            <div className="space-y-4">
              <p className="text-orange-600 dark:text-orange-400 mb-4">
                {device?.isMobile ? t('invite.inviteLanding.mobileComingSoon') : t('invite.inviteLanding.platformComingSoon')}
              </p>
              <Button variant="outline" size="lg" disabled>
                <span className="mr-2">{t('invite.inviteLanding.hourglassIcon')}</span>
                {t('invite.inviteLanding.comingSoon')}
              </Button>
            </div>
          )}

          <p className="text-sm text-gray-500 dark:text-gray-400 mt-6">
            {t('invite.inviteLanding.downloadInstructions')} <code className="font-mono font-bold text-blue-600">{inviteInfo.code.toUpperCase()}</code> {t('invite.inviteLanding.registerToGetReward')}
          </p>
        </div>
      </Card>

      {/* 其他平台下载选项 */}
      <Card className="p-6">
        <h4 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 text-center">
          {t('invite.inviteLanding.otherPlatforms')}
        </h4>

        <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {/* Windows */}
          <div className="text-center p-4 rounded-lg border border-gray-200 dark:border-gray-700">
            <Monitor className="w-8 h-8 text-purple-600 mx-auto mb-2" />
            <h5 className="font-semibold mb-1">{t('invite.inviteLanding.windows')}</h5>
            <p className="text-xs text-gray-500 mb-3">{t('invite.inviteLanding.windowsVersion')}</p>
            {DOWNLOAD_LINKS.windows ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.windows)}
              >
                {t('invite.inviteLanding.download')}
              </Button>
            ) : (
              <Button variant="outline" size="sm" disabled>
                {t('invite.inviteLanding.comingSoon')}
              </Button>
            )}
          </div>

          {/* macOS */}
          <div className="text-center p-4 rounded-lg border border-gray-200 dark:border-gray-700">
            <Monitor className="w-8 h-8 text-gray-600 mx-auto mb-2" />
            <h5 className="font-semibold mb-1">{t('invite.inviteLanding.macos')}</h5>
            <p className="text-xs text-gray-500 mb-3">{t('invite.inviteLanding.macosVersion')}</p>
            {DOWNLOAD_LINKS.macos ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.macos)}
              >
                {t('invite.inviteLanding.download')}
              </Button>
            ) : (
              <Button variant="outline" size="sm" disabled>
                {t('invite.inviteLanding.comingSoon')}
              </Button>
            )}
          </div>

          {/* iOS */}
          <div className="text-center p-4 rounded-lg border border-gray-200 dark:border-gray-700">
            <Smartphone className="w-8 h-8 text-blue-600 mx-auto mb-2" />
            <h5 className="font-semibold mb-1">{t('invite.inviteLanding.ios')}</h5>
            <p className="text-xs text-gray-500 mb-3">{t('invite.inviteLanding.iosDevices')}</p>
            {DOWNLOAD_LINKS.ios ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.ios)}
              >
                {t('invite.inviteLanding.appStore')}
              </Button>
            ) : (
              <Button variant="outline" size="sm" disabled>
                {t('invite.inviteLanding.comingSoon')}
              </Button>
            )}
          </div>

          {/* Android */}
          <div className="text-center p-4 rounded-lg border border-gray-200 dark:border-gray-700">
            <Smartphone className="w-8 h-8 text-green-600 mx-auto mb-2" />
            <h5 className="font-semibold mb-1">{t('invite.inviteLanding.android')}</h5>
            <p className="text-xs text-gray-500 mb-3">{t('invite.inviteLanding.androidVersion')}</p>
            {DOWNLOAD_LINKS.android ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.android)}
              >
                {t('invite.inviteLanding.download')}
              </Button>
            ) : (
              <Button variant="outline" size="sm" disabled>
                {t('invite.inviteLanding.comingSoon')}
              </Button>
            )}
          </div>
        </div>
      </Card>

      {/* 使用步骤 */}
      <Card className="p-8 mt-8 bg-gray-50 dark:bg-gray-800/50">
        <h4 className="text-xl font-bold text-gray-900 dark:text-white mb-6 text-center">
          {t('invite.inviteLanding.stepsTitle')}
        </h4>

        <div className="grid md:grid-cols-3 gap-6">
          <div className="text-center">
            <div className="w-12 h-12 bg-blue-600 text-white rounded-full flex items-center justify-center mx-auto mb-3 text-xl font-bold">
              {"1"}
            </div>
            <h5 className="font-semibold mb-2">{t('invite.inviteLanding.step1Title')}</h5>
            <p className="text-sm text-gray-600 dark:text-gray-300">
              {t('invite.inviteLanding.step1Desc')}
            </p>
          </div>

          <div className="text-center">
            <div className="w-12 h-12 bg-purple-600 text-white rounded-full flex items-center justify-center mx-auto mb-3 text-xl font-bold">
              {"2"}
            </div>
            <h5 className="font-semibold mb-2">{t('invite.inviteLanding.step2Title')}</h5>
            <p className="text-sm text-gray-600 dark:text-gray-300">
              {t('invite.inviteLanding.step2Desc')} <code className="font-mono text-blue-600">{inviteInfo.code.toUpperCase()}</code> {t('invite.inviteLanding.step2Desc2')}
            </p>
          </div>

          <div className="text-center">
            <div className="w-12 h-12 bg-green-600 text-white rounded-full flex items-center justify-center mx-auto mb-3 text-xl font-bold">
              {"3"}
            </div>
            <h5 className="font-semibold mb-2">{t('invite.inviteLanding.step3Title')}</h5>
            <p className="text-sm text-gray-600 dark:text-gray-300">
              {t('invite.inviteLanding.step3Desc')}
            </p>
          </div>
        </div>
      </Card>
    </div>
  );
}
