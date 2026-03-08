"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS, BETA_VERSION, DESKTOP_VERSION, getDownloadLinks } from '@/lib/constants';
import {
  detectDevice,
  triggerDownload,
  openDownloadInNewTab,
  DeviceInfo
} from '@/lib/device-detection';
import {
  Download,
  CheckCircle,
  AlertCircle,
  RefreshCw,
  ArrowRight,
  ExternalLink,
} from 'lucide-react';
import { Link } from '@/i18n/routing';
import Image from 'next/image';

type DownloadState = 'detecting' | 'ready' | 'downloading' | 'success' | 'failed' | 'cancelled' | 'unavailable';

interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
}

function isPrerelease(version: string): boolean {
  return /-(alpha|beta|rc|dev)/.test(version);
}

function DownloadButton({ href, backupHref, label, t, variant = 'default' }: {
  href: string;
  backupHref: string;
  label: string;
  t: ReturnType<typeof useTranslations>;
  variant?: 'default' | 'outline';
}) {
  return (
    <div className="flex flex-col items-center gap-1">
      <Button
        variant={variant}
        size="sm"
        className={variant === 'default' ? 'bg-blue-600 hover:bg-blue-700 w-full' : 'w-full'}
        onClick={() => openDownloadInNewTab(href)}
      >
        <Download className="w-4 h-4 mr-2" />
        {label}
      </Button>
      <a
        href={backupHref}
        target="_blank"
        rel="noopener noreferrer"
        className="text-xs text-muted-foreground hover:text-foreground hover:underline transition-colors"
      >
        {t('install.install.backupDownload')}
      </a>
    </div>
  );
}

function PlatformCard({ icon, name, subtitle, children, isDetected }: {
  icon: string;
  name: string;
  subtitle?: string;
  children: React.ReactNode;
  isDetected?: boolean;
}) {
  return (
    <Card className={`p-5 flex flex-col items-center text-center gap-3 ${isDetected ? 'border-blue-600 ring-1 ring-blue-600' : ''}`}>
      <Image src={icon} alt={name} width={40} height={40} className="opacity-80" />
      <div>
        <h4 className="font-semibold text-foreground">{name}</h4>
        {subtitle && <p className="text-xs text-muted-foreground">{subtitle}</p>}
      </div>
      <div className="w-full space-y-2 mt-auto">
        {children}
      </div>
    </Card>
  );
}

export default function InstallClient({ betaVersion: serverBeta, stableVersion: serverStable }: InstallClientProps) {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);

  const effectiveBeta = serverBeta || BETA_VERSION;
  const effectiveStable = serverStable || DESKTOP_VERSION;

  const betaIsPrerelease = isPrerelease(effectiveBeta);
  const showBetaAndStable = betaIsPrerelease && effectiveStable !== effectiveBeta;

  const betaLinks = getDownloadLinks(effectiveBeta);
  const stableLinks = showBetaAndStable ? getDownloadLinks(effectiveStable) : null;

  // Determine primary download link based on device (beta preferred)
  const getPrimaryLink = useCallback((deviceInfo: DeviceInfo | null) => {
    if (!deviceInfo) return null;
    switch (deviceInfo.type) {
      case 'windows': return betaLinks.windows.primary;
      case 'macos': return betaLinks.macos.primary;
      default: return null;
    }
  }, [betaLinks]);

  useEffect(() => {
    const deviceInfo = detectDevice();
    setDevice(deviceInfo);
    if (deviceInfo.isDesktop) {
      setDownloadState('ready');
    } else {
      setDownloadState('unavailable');
    }
  }, []);

  const primaryLink = getPrimaryLink(device);

  const startDownload = useCallback(async () => {
    if (!primaryLink) return;
    setDownloadState('downloading');
    const filename = primaryLink.split('/').pop() || undefined;
    const downloadTriggered = triggerDownload(primaryLink, filename);
    if (downloadTriggered) {
      setTimeout(() => setDownloadState('success'), 2000);
    } else {
      openDownloadInNewTab(primaryLink);
      setDownloadState('failed');
    }
  }, [primaryLink]);

  useEffect(() => {
    if (downloadState === 'ready' && countdown > 0) {
      const timer = setTimeout(() => setCountdown(countdown - 1), 1000);
      return () => clearTimeout(timer);
    } else if (downloadState === 'ready' && countdown === 0) {
      startDownload();
    }
  }, [downloadState, countdown, startDownload]);

  const retryDownload = () => {
    setCountdown(5);
    setDownloadState('ready');
  };

  const versionLabel = showBetaAndStable
    ? t('install.install.betaVersion', { version: effectiveBeta })
    : t('install.install.latestVersion', { version: effectiveBeta });

  return (
    <>
      {/* Hero: Device Detection + Auto Download */}
      <Card className="p-8 mb-8">
        <div className="text-center">
          {device && (
            <Image
              src={`/icons/platforms/${device.type === 'windows' ? 'windows' : device.type === 'macos' ? 'macos' : device.type === 'ios' ? 'ios' : device.type === 'android' ? 'android' : 'windows'}.svg`}
              alt={device.name}
              width={48}
              height={48}
              className="mx-auto mb-4 opacity-70"
            />
          )}

          <h2 className="text-2xl font-bold text-foreground mb-1">
            {device ? t('install.install.deviceDetected', { device: device.name }) : t('install.install.detectingDevice')}
          </h2>

          {device?.isDesktop && (
            <p className="text-sm text-muted-foreground mb-2">
              {versionLabel}
              {showBetaAndStable && (
                <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-500/15 text-blue-400">
                  {t('install.install.recommended')}
                </span>
              )}
            </p>
          )}

          {downloadState === 'detecting' && (
            <p className="text-muted-foreground">{t('install.install.analyzingDevice')}</p>
          )}
        </div>
      </Card>

      {/* Download State Cards */}
      {downloadState === 'ready' && (
        <Card className="p-8 mb-8 border-blue-800 bg-blue-900/20">
          <div className="text-center">
            <RefreshCw className="w-12 h-12 text-blue-600 mx-auto mb-4 animate-spin" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.readyToDownload')}
            </h3>
            <p className="text-blue-200 mb-4">
              {t('install.install.autoDownloadCountdown', { seconds: countdown })}
            </p>
            <div className="flex justify-center space-x-4">
              <Button onClick={startDownload} className="bg-blue-600 hover:bg-blue-700">
                <Download className="w-4 h-4 mr-2" />
                {t('install.install.downloadNow')}
              </Button>
              <Button variant="outline" onClick={() => setDownloadState('cancelled')}>
                {t('install.install.cancelAutoDownload')}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {downloadState === 'downloading' && (
        <Card className="p-8 mb-8 border-yellow-800 bg-yellow-900/20">
          <div className="text-center">
            <Download className="w-12 h-12 text-yellow-600 mx-auto mb-4 animate-bounce" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloading')}
            </h3>
            <p className="text-yellow-200">{t('install.install.checkDownloadLocation')}</p>
          </div>
        </Card>
      )}

      {downloadState === 'success' && (
        <Card className="p-8 mb-8 border-green-800 bg-green-900/20">
          <div className="text-center">
            <CheckCircle className="w-12 h-12 text-green-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloadSuccess')}
            </h3>
            <p className="text-green-200 mb-4">{t('install.install.runInstaller')}</p>
            <div className="flex justify-center space-x-4">
              <Button onClick={retryDownload} variant="outline">
                <RefreshCw className="w-4 h-4 mr-2" />
                {t('install.install.redownload')}
              </Button>
              <Link href="/">
                <Button className="bg-green-600 hover:bg-green-700">
                  {t('install.install.backToHome')}
                  <ArrowRight className="w-4 h-4 ml-2" />
                </Button>
              </Link>
            </div>
          </div>
        </Card>
      )}

      {downloadState === 'failed' && (
        <Card className="p-8 mb-8 border-red-800 bg-red-900/20">
          <div className="text-center">
            <AlertCircle className="w-12 h-12 text-red-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloadFailed')}
            </h3>
            <p className="text-red-200 mb-4">{t('install.install.downloadFailedMessage')}</p>
            <div className="flex justify-center space-x-4">
              <Button onClick={() => primaryLink && openDownloadInNewTab(primaryLink)} className="bg-red-600 hover:bg-red-700">
                <ExternalLink className="w-4 h-4 mr-2" />
                {t('install.install.manualDownload')}
              </Button>
              <Button onClick={retryDownload} variant="outline">
                <RefreshCw className="w-4 h-4 mr-2" />
                {t('install.install.retryAutoDownload')}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {/* Cancelled -- desktop user can still download manually */}
      {downloadState === 'cancelled' && device?.isDesktop && primaryLink && (
        <Card className="p-8 mb-8">
          <div className="text-center">
            <h3 className="text-lg font-semibold text-foreground mb-2">
              {t('install.install.downloadCancelled')}
            </h3>
            <p className="text-sm text-muted-foreground mb-4">{versionLabel}</p>
            <Button onClick={startDownload} className="bg-blue-600 hover:bg-blue-700">
              <Download className="w-4 h-4 mr-2" />
              {t('install.install.clickToDownload')}
            </Button>
          </div>
        </Card>
      )}

      {/* Mobile unavailable */}
      {downloadState === 'unavailable' && device?.isMobile && (
        <Card className="p-8 mb-8 border-orange-800 bg-orange-900/20">
          <div className="text-center">
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.mobileComingSoon')}
            </h3>
            <p className="text-orange-200 mb-4">
              {t('install.install.platformDevelopment', {
                platform: device.type === 'ios' ? t('install.install.ios') : t('install.install.android')
              })}
            </p>
            <p className="text-sm text-orange-300">{t('install.install.useDesktopVersion')}</p>
          </div>
        </Card>
      )}

      {/* All Platforms Grid */}
      <h3 className="text-lg font-semibold text-foreground mb-4 mt-8">
        {t('install.install.allDownloadOptions')}
      </h3>
      <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        {/* Windows */}
        <PlatformCard
          icon="/icons/platforms/windows.svg"
          name={t('install.install.windows')}
          subtitle={t('install.install.windowsVersion')}
          isDetected={device?.type === 'windows'}
        >
          <DownloadButton
            href={betaLinks.windows.primary}
            backupHref={betaLinks.windows.backup}
            label={showBetaAndStable
              ? t('install.install.betaVersion', { version: effectiveBeta })
              : t('install.install.latestVersion', { version: effectiveBeta })}
            t={t}
          />
          {showBetaAndStable && stableLinks && (
            <DownloadButton
              href={stableLinks.windows.primary}
              backupHref={stableLinks.windows.backup}
              label={t('install.install.stableVersion', { version: effectiveStable })}
              t={t}
              variant="outline"
            />
          )}
        </PlatformCard>

        {/* macOS */}
        <PlatformCard
          icon="/icons/platforms/macos.svg"
          name={t('install.install.macos')}
          subtitle={t('install.install.macosVersion')}
          isDetected={device?.type === 'macos'}
        >
          <DownloadButton
            href={betaLinks.macos.primary}
            backupHref={betaLinks.macos.backup}
            label={showBetaAndStable
              ? t('install.install.betaVersion', { version: effectiveBeta })
              : t('install.install.latestVersion', { version: effectiveBeta })}
            t={t}
          />
          {showBetaAndStable && stableLinks && (
            <DownloadButton
              href={stableLinks.macos.primary}
              backupHref={stableLinks.macos.backup}
              label={t('install.install.stableVersion', { version: effectiveStable })}
              t={t}
              variant="outline"
            />
          )}
        </PlatformCard>

        {/* iOS */}
        <PlatformCard
          icon="/icons/platforms/ios.svg"
          name={t('install.install.ios')}
          subtitle={t('install.install.iosDevices')}
          isDetected={device?.type === 'ios'}
        >
          {DOWNLOAD_LINKS.ios && (
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.ios)}
            >
              <ExternalLink className="w-4 h-4 mr-2" />
              {t('install.install.appStore')}
              <span className="ml-1 text-muted-foreground">({t('install.install.waymakerApp')})</span>
            </Button>
          )}
          <p className="text-xs text-muted-foreground">{t('install.install.kaituMobileComingSoon')}</p>
        </PlatformCard>

        {/* Android */}
        <PlatformCard
          icon="/icons/platforms/android.svg"
          name={t('install.install.android')}
          subtitle={t('install.install.androidVersion')}
          isDetected={device?.type === 'android'}
        >
          {DOWNLOAD_LINKS.android && (
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.android)}
            >
              <Download className="w-4 h-4 mr-2" />
              {t('install.install.downloadApk')}
              <span className="ml-1 text-muted-foreground">({t('install.install.waymakerApp')})</span>
            </Button>
          )}
          <p className="text-xs text-muted-foreground">{t('install.install.kaituMobileComingSoon')}</p>
        </PlatformCard>
      </div>

      {/* View all releases link */}
      <div className="text-center mt-8">
        <Link href="/releases" className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1">
          {t('install.install.viewAllReleases')}
          <ArrowRight className="w-3 h-3" />
        </Link>
      </div>
    </>
  );
}
