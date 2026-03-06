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
  Smartphone,
  Monitor,
  Apple,
  ExternalLink,
  RefreshCw,
  ArrowRight
} from 'lucide-react';
import { Link } from '@/i18n/routing';

type DownloadState = 'detecting' | 'ready' | 'downloading' | 'success' | 'failed' | 'unavailable';

interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
}

export default function InstallClient({ betaVersion: serverBeta, stableVersion: serverStable }: InstallClientProps) {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);

  // Server-fetched version takes priority, build-time env var as fallback
  const effectiveBeta = serverBeta || BETA_VERSION;
  const effectiveStable = serverStable || DESKTOP_VERSION;

  // Beta is the primary download for desktop
  const betaLinks = getDownloadLinks(effectiveBeta);
  const stableLinks = getDownloadLinks(effectiveStable);

  // Determine primary download link based on device
  const getPrimaryLink = useCallback((deviceInfo: DeviceInfo | null) => {
    if (!deviceInfo) return null;
    switch (deviceInfo.type) {
      case 'windows': return betaLinks.windows;
      case 'macos': return betaLinks.macos;
      case 'ios': return DOWNLOAD_LINKS.ios;
      case 'android': return DOWNLOAD_LINKS.android;
      default: return null;
    }
  }, [betaLinks]);

  // Detect device on mount
  useEffect(() => {
    const deviceInfo = detectDevice();
    setDevice(deviceInfo);

    if (deviceInfo.isDesktop) {
      setDownloadState('ready');
    } else if (deviceInfo.isMobile) {
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

  // Auto-download countdown
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

  const manualDownload = () => {
    if (primaryLink) openDownloadInNewTab(primaryLink);
  };

  return (
    <>
      {/* Hero: Device Detection + Beta Download */}
      <Card className="p-8 mb-8">
        <div className="text-center">
          <div className="w-16 h-16 mx-auto mb-4 bg-blue-900/30 rounded-full flex items-center justify-center">
            {device?.isMobile ? (
              <Smartphone className="w-8 h-8 text-blue-600" />
            ) : device?.isDesktop ? (
              <Monitor className="w-8 h-8 text-blue-600" />
            ) : (
              <Download className="w-8 h-8 text-blue-600" />
            )}
          </div>

          <h2 className="text-2xl font-bold text-foreground mb-1">
            {device ? t('install.install.deviceDetected', { device: device.name }) : t('install.install.detectingDevice')}
          </h2>

          {device?.isDesktop && (
            <p className="text-sm text-muted-foreground mb-2">
              {t('install.install.betaVersion', { version: effectiveBeta })}
            </p>
          )}

          {downloadState === 'detecting' && (
            <p className="text-muted-foreground">
              {t('install.install.analyzingDevice')}
            </p>
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
              <Button variant="outline" onClick={() => setDownloadState('unavailable')}>
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
            <p className="text-yellow-200">
              {t('install.install.checkDownloadLocation')}
            </p>
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
            <p className="text-green-200 mb-4">
              {t('install.install.runInstaller')}
            </p>
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
            <p className="text-red-200 mb-4">
              {t('install.install.downloadFailedMessage')}
            </p>
            <div className="flex justify-center space-x-4">
              <Button onClick={manualDownload} className="bg-red-600 hover:bg-red-700">
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

      {/* Mobile unavailable */}
      {downloadState === 'unavailable' && device?.isMobile && (
        <Card className="p-8 mb-8 border-orange-800 bg-orange-900/20">
          <div className="text-center">
            <Smartphone className="w-12 h-12 text-orange-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.mobileComingSoon')}
            </h3>
            <p className="text-orange-200 mb-4">
              {t('install.install.platformDevelopment', { platform: device.type === 'ios' ? t('install.install.ios') : t('install.install.android') })}
            </p>
            <p className="text-sm text-orange-300">
              {t('install.install.useDesktopVersion')}
            </p>
          </div>
        </Card>
      )}

      {/* Stable version section */}
      <Card className="p-6 mb-8 bg-muted/50">
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4">
          <div>
            <h3 className="text-lg font-semibold text-foreground">
              {t('install.install.lookingForStable')}
            </h3>
            <p className="text-sm text-muted-foreground">
              {t('install.install.stableVersion', { version: effectiveStable })}
            </p>
          </div>
          <div className="flex gap-3">
            <Button
              variant="outline"
              size="sm"
              onClick={() => openDownloadInNewTab(stableLinks.windows)}
            >
              <Monitor className="w-4 h-4 mr-2" />
              {t('install.install.downloadExe')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => openDownloadInNewTab(stableLinks.macos)}
            >
              <Apple className="w-4 h-4 mr-2" />
              {t('install.install.downloadDmg')}
            </Button>
          </div>
        </div>
      </Card>

      {/* All platforms grid */}
      <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6">
        {/* Windows */}
        <Button
          variant="outline"
          size="sm"
          onClick={() => openDownloadInNewTab(betaLinks.windows)}
          className={device?.type === 'windows' ? 'border-secondary bg-secondary/10' : ''}
        >
          <Download className="w-4 h-4 mr-2" />
          {t('install.install.downloadExe')}
        </Button>

        {/* macOS */}
        <Button
          variant="outline"
          size="sm"
          onClick={() => openDownloadInNewTab(betaLinks.macos)}
          className={device?.type === 'macos' ? 'border-border bg-muted' : ''}
        >
          <Download className="w-4 h-4 mr-2" />
          {t('install.install.downloadDmg')}
        </Button>

        {/* iOS */}
        {DOWNLOAD_LINKS.ios ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.ios)}
            className={device?.type === 'ios' ? 'border-blue-500 bg-blue-50' : ''}
          >
            <ExternalLink className="w-4 h-4 mr-2" />
            {t('install.install.appStore')}
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            {t('install.install.comingSoon')}
          </Button>
        )}

        {/* Android */}
        {DOWNLOAD_LINKS.android ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.android)}
            className={device?.type === 'android' ? 'border-green-500 bg-green-50' : ''}
          >
            <Download className="w-4 h-4 mr-2" />
            {t('install.install.downloadApk')}
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            {t('install.install.comingSoon')}
          </Button>
        )}
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
