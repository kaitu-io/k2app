"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS } from '@/lib/constants';
import {
  detectDevice,
  getPrimaryDownloadLink,
  hasAvailableDownload,
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
  ExternalLink,
  RefreshCw,
  ArrowRight
} from 'lucide-react';
import Link from 'next/link';

type DownloadState = 'detecting' | 'ready' | 'downloading' | 'success' | 'failed' | 'unavailable';

/**
 * InstallClient — Client Component for the install page.
 *
 * Handles all browser-dependent logic:
 * - Device detection via navigator.userAgent
 * - Auto-download countdown
 * - Download button click handlers
 * - triggerDownload / openDownloadInNewTab browser APIs
 *
 * Receives translated strings and download links as props from the
 * Server Component shell (install/page.tsx).
 */
export default function InstallClient() {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);
  const [primaryLink, setPrimaryLink] = useState<string | null>(null);

  // Detect device on mount
  useEffect(() => {
    const deviceInfo = detectDevice();
    const primaryDownloadLink = getPrimaryDownloadLink(DOWNLOAD_LINKS);

    setDevice(deviceInfo);
    setPrimaryLink(primaryDownloadLink);

    if (hasAvailableDownload(DOWNLOAD_LINKS)) {
      setDownloadState('ready');
    } else {
      setDownloadState('unavailable');
    }
  }, []);

  const startDownload = useCallback(async () => {
    if (!primaryLink) return;

    setDownloadState('downloading');

    // Extract filename from URL
    const filename = primaryLink.split('/').pop() || undefined;

    // Try automatic download
    const downloadTriggered = triggerDownload(primaryLink, filename);

    if (downloadTriggered) {
      // Assume success after a short delay
      setTimeout(() => {
        setDownloadState('success');
      }, 2000);
    } else {
      // Fallback to opening in new tab
      openDownloadInNewTab(primaryLink);
      setDownloadState('failed');
    }
  }, [primaryLink]);

  // Auto-download countdown
  useEffect(() => {
    if (downloadState === 'ready' && countdown > 0) {
      const timer = setTimeout(() => {
        setCountdown(countdown - 1);
      }, 1000);
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
    if (primaryLink) {
      openDownloadInNewTab(primaryLink);
    }
  };

  return (
    <>
      {/* Device Detection Status */}
      <Card className="p-8 mb-8">
        <div className="text-center">
          <div className="w-16 h-16 mx-auto mb-4 bg-blue-100 dark:bg-blue-900 rounded-full flex items-center justify-center">
            {device?.isMobile ? (
              <Smartphone className="w-8 h-8 text-blue-600" />
            ) : device?.isDesktop ? (
              <Monitor className="w-8 h-8 text-blue-600" />
            ) : (
              <Download className="w-8 h-8 text-blue-600" />
            )}
          </div>

          <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">
            {device ? t('install.install.deviceDetected', {device: device.name}) : t('install.install.detectingDevice')}
          </h2>

          {downloadState === 'detecting' && (
            <p className="text-gray-600 dark:text-gray-300">
              {t('install.install.analyzingDevice')}
            </p>
          )}
        </div>
      </Card>

      {/* Download State Cards */}
      {downloadState === 'ready' && (
        <Card className="p-8 mb-8 border-blue-200 bg-blue-50 dark:bg-blue-900/20">
          <div className="text-center">
            <RefreshCw className="w-12 h-12 text-blue-600 mx-auto mb-4 animate-spin" />
            <h3 className="text-xl font-bold text-blue-900 dark:text-blue-100 mb-2">
              {t('install.install.readyToDownload')}
            </h3>
            <p className="text-blue-700 dark:text-blue-200 mb-4">
              {t('install.install.autoDownloadCountdown', {seconds: countdown})}
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
        <Card className="p-8 mb-8 border-yellow-200 bg-yellow-50 dark:bg-yellow-900/20">
          <div className="text-center">
            <Download className="w-12 h-12 text-yellow-600 mx-auto mb-4 animate-bounce" />
            <h3 className="text-xl font-bold text-yellow-900 dark:text-yellow-100 mb-2">
              {t('install.install.downloading')}
            </h3>
            <p className="text-yellow-700 dark:text-yellow-200">
              {t('install.install.checkDownloadLocation')}
            </p>
          </div>
        </Card>
      )}

      {downloadState === 'success' && (
        <Card className="p-8 mb-8 border-green-200 bg-green-50 dark:bg-green-900/20">
          <div className="text-center">
            <CheckCircle className="w-12 h-12 text-green-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-green-900 dark:text-green-100 mb-2">
              {t('install.install.downloadSuccess')}
            </h3>
            <p className="text-green-700 dark:text-green-200 mb-4">
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
        <Card className="p-8 mb-8 border-red-200 bg-red-50 dark:bg-red-900/20">
          <div className="text-center">
            <AlertCircle className="w-12 h-12 text-red-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-red-900 dark:text-red-100 mb-2">
              {t('install.install.downloadFailed')}
            </h3>
            <p className="text-red-700 dark:text-red-200 mb-4">
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

      {downloadState === 'unavailable' && device?.isMobile && (
        <Card className="p-8 mb-8 border-orange-200 bg-orange-50 dark:bg-orange-900/20">
          <div className="text-center">
            <Smartphone className="w-12 h-12 text-orange-600 mx-auto mb-4" />
            <h3 className="text-xl font-bold text-orange-900 dark:text-orange-100 mb-2">
              {t('install.install.mobileComingSoon')}
            </h3>
            <p className="text-orange-700 dark:text-orange-200 mb-4">
              {t('install.install.platformDevelopment', {platform: device.type === 'ios' ? 'iOS' : 'Android'})}
            </p>
            <p className="text-sm text-orange-600 dark:text-orange-300">
              {t('install.install.useDesktopVersion')}
            </p>
          </div>
        </Card>
      )}

      {/* Dynamic download buttons for all platform cards */}
      <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6 mt-6">
        {/* Windows dynamic button */}
        {DOWNLOAD_LINKS.windows ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.windows)}
            className={device?.type === 'windows' ? 'border-purple-500 bg-purple-50' : ''}
          >
            <Download className="w-4 h-4 mr-2" />
            {t('install.install.downloadExe')}
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            <span className="mr-2">{"⏳"}</span>
            {t('install.install.comingSoon')}
          </Button>
        )}

        {/* macOS dynamic button */}
        {DOWNLOAD_LINKS.macos ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.macos)}
            className={device?.type === 'macos' ? 'border-gray-500 bg-gray-50' : ''}
          >
            <Download className="w-4 h-4 mr-2" />
            {t('install.install.downloadDmg')}
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            <span className="mr-2">{"⏳"}</span>
            {t('install.install.comingSoon')}
          </Button>
        )}

        {/* iOS dynamic button */}
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
          <Button
            variant="outline"
            size="sm"
            disabled
            className={device?.type === 'ios' ? 'border-blue-300' : ''}
          >
            <span className="mr-2">{"⏳"}</span>
            {t('install.install.comingSoon')}
          </Button>
        )}

        {/* Android dynamic button */}
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
          <Button
            variant="outline"
            size="sm"
            disabled
            className={device?.type === 'android' ? 'border-green-300' : ''}
          >
            <span className="mr-2">{"⏳"}</span>
            {t('install.install.comingSoon')}
          </Button>
        )}
      </div>
    </>
  );
}
