"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS, DESKTOP_VERSION, getDownloadLinks } from '@/lib/constants';
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

const platformIcons: Record<string, React.FC<{ className?: string }>> = {
  windows: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M2 6.5L20.3 3.8V22.5H2V6.5ZM22.5 3.5L46 0V22.5H22.5V3.5ZM2 24.5H20.3V43.2L2 40.5V24.5ZM22.5 24.5H46V47L22.5 43.5V24.5Z"/>
    </svg>
  ),
  macos: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M39.6 25.2c-.1-4.4 3.6-6.5 3.8-6.6-2.1-3-5.3-3.5-6.4-3.5-2.7-.3-5.4 1.6-6.7 1.6-1.4 0-3.5-1.6-5.8-1.5-3 0-5.7 1.7-7.3 4.4-3.1 5.4-.8 13.5 2.2 17.9 1.5 2.1 3.3 4.6 5.6 4.5 2.2-.1 3.1-1.5 5.8-1.5 2.7 0 3.5 1.5 5.8 1.4 2.4 0 4-2.2 5.4-4.3 1.7-2.5 2.4-4.9 2.5-5-.1 0-4.7-1.8-4.9-7.4zM35.1 11.9c1.2-1.5 2.1-3.5 1.8-5.5-1.8.1-3.9 1.2-5.2 2.7-1.1 1.3-2.1 3.4-1.9 5.4 2 .2 4-1 5.3-2.6z"/>
    </svg>
  ),
  ios: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <rect x="12" y="2" width="24" height="44" rx="5" ry="5" fill="none" stroke="currentColor" strokeWidth="2.5"/>
      <rect x="19" y="4" width="10" height="3" rx="1.5"/>
      <circle cx="24" cy="40" r="2"/>
    </svg>
  ),
  android: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M15.4 8.8l-2.9-5c-.2-.4-.1-.8.3-1 .4-.2.8-.1 1 .3l2.9 5.1c2.2-1 4.7-1.5 7.3-1.5s5.1.6 7.3 1.5l2.9-5.1c.2-.4.6-.5 1-.3.4.2.5.6.3 1l-2.9 5c5.1 2.5 8.5 7.3 8.5 12.8H6.9c0-5.5 3.4-10.3 8.5-12.8zM18 16.5c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zm12 0c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zM6.9 24h34.2v16c0 1.7-1.3 3-3 3H9.9c-1.7 0-3-1.3-3-3V24z"/>
    </svg>
  ),
};

function PlatformIcon({ type, className }: { type: string; className?: string }) {
  const Icon = platformIcons[type] || platformIcons.windows;
  return <Icon className={className} />;
}

type DownloadState = 'detecting' | 'ready' | 'downloading' | 'success' | 'failed' | 'cancelled' | 'unavailable';

interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
}

function DownloadButton({ href, label, channelLabel, variant = 'default' }: {
  href: string;
  label: string;
  channelLabel?: string;
  variant?: 'default' | 'outline';
}) {
  return (
    <div className="flex flex-col items-center gap-0.5">
      {channelLabel && (
        <span className="text-[10px] text-muted-foreground">{channelLabel}</span>
      )}
      <Button
        variant={variant}
        size="sm"
        className="w-full text-xs"
        onClick={() => openDownloadInNewTab(href)}
      >
        <Download className="w-3.5 h-3.5 mr-1.5 shrink-0" />
        <span className="truncate">{label}</span>
      </Button>
    </div>
  );
}

function PlatformCard({ platform, name, subtitle, children, isDetected }: {
  platform: string;
  name: string;
  subtitle?: string;
  children: React.ReactNode;
  isDetected?: boolean;
}) {
  return (
    <Card className={`p-5 flex flex-col items-center text-center gap-3 ${isDetected ? 'border-primary ring-1 ring-primary' : ''}`}>
      <PlatformIcon type={platform} className="w-10 h-10 text-foreground opacity-80" />
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

export default function InstallClient({ stableVersion: serverStable }: InstallClientProps) {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);

  // TODO: Temporarily hide beta channel — show only stable version
  const effectiveStable = serverStable || DESKTOP_VERSION;
  const showBetaAndStable = false;
  const betaLinks = getDownloadLinks(effectiveStable); // Use stable version as primary
  const stableLinks = null as ReturnType<typeof getDownloadLinks> | null;

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

  const versionLabel = t('install.install.latestVersion', { version: effectiveStable });

  return (
    <>
      {/* Hero: Device Detection + Auto Download */}
      <Card className="p-8 mb-8">
        <div className="text-center">
          {device && (
            <PlatformIcon type={device.type} className="w-12 h-12 mx-auto mb-4 text-foreground opacity-70" />
          )}

          <h2 className="text-2xl font-bold text-foreground mb-1">
            {device ? t('install.install.deviceDetected', { device: device.name }) : t('install.install.detectingDevice')}
          </h2>

          {device?.isDesktop && (
            <p className="text-sm text-muted-foreground mb-2">
              {versionLabel}
              {showBetaAndStable && (
                <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
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
        <Card className="p-8 mb-8 border-info/30 bg-info/10">
          <div className="text-center">
            <RefreshCw className="w-12 h-12 text-info mx-auto mb-4 animate-spin" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.readyToDownload')}
            </h3>
            <p className="text-info mb-4">
              {t('install.install.autoDownloadCountdown', { seconds: countdown })}
            </p>
            <div className="flex justify-center space-x-4">
              <Button onClick={startDownload}>
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
        <Card className="p-8 mb-8 border-warning/30 bg-warning/10">
          <div className="text-center">
            <Download className="w-12 h-12 text-warning mx-auto mb-4 animate-bounce" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloading')}
            </h3>
            <p className="text-warning">{t('install.install.checkDownloadLocation')}</p>
          </div>
        </Card>
      )}

      {downloadState === 'success' && (
        <Card className="p-8 mb-8 border-success/30 bg-success/10">
          <div className="text-center">
            <CheckCircle className="w-12 h-12 text-success mx-auto mb-4" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloadSuccess')}
            </h3>
            <p className="text-success mb-4">{t('install.install.runInstaller')}</p>
            <div className="flex justify-center space-x-4">
              <Button onClick={retryDownload} variant="outline">
                <RefreshCw className="w-4 h-4 mr-2" />
                {t('install.install.redownload')}
              </Button>
              <Link href="/">
                <Button>
                  {t('install.install.backToHome')}
                  <ArrowRight className="w-4 h-4 ml-2" />
                </Button>
              </Link>
            </div>
          </div>
        </Card>
      )}

      {downloadState === 'failed' && (
        <Card className="p-8 mb-8 border-destructive/30 bg-destructive/10">
          <div className="text-center">
            <AlertCircle className="w-12 h-12 text-destructive mx-auto mb-4" />
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.downloadFailed')}
            </h3>
            <p className="text-destructive mb-4">{t('install.install.downloadFailedMessage')}</p>
            <div className="flex justify-center space-x-4">
              <Button onClick={() => primaryLink && openDownloadInNewTab(primaryLink)} variant="destructive">
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
            <Button onClick={startDownload}>
              <Download className="w-4 h-4 mr-2" />
              {t('install.install.clickToDownload')}
            </Button>
          </div>
        </Card>
      )}

      {/* Mobile unavailable */}
      {downloadState === 'unavailable' && device?.isMobile && (
        <Card className="p-8 mb-8 border-warning/30 bg-warning/10">
          <div className="text-center">
            <h3 className="text-xl font-bold text-foreground mb-2">
              {t('install.install.mobileComingSoon')}
            </h3>
            <p className="text-warning mb-4">
              {t('install.install.platformDevelopment', {
                platform: device.type === 'ios' ? t('install.install.ios') : t('install.install.android')
              })}
            </p>
            <p className="text-sm text-warning/80">{t('install.install.useDesktopVersion')}</p>
          </div>
        </Card>
      )}

      {/* All Platforms Grid */}
      <h3 className="text-lg font-semibold text-foreground mb-4 mt-8">
        {t('install.install.allDownloadOptions')}
      </h3>
      <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8 items-stretch">
        {/* Windows */}
        <PlatformCard
          platform="windows"
          name={t('install.install.windows')}
          subtitle={t('install.install.windowsVersion')}
          isDetected={device?.type === 'windows'}
        >
          <DownloadButton
            href={betaLinks.windows.primary}
            label={`v${effectiveStable}`}
            channelLabel={showBetaAndStable ? t('install.install.recommended') : undefined}
          />
          {showBetaAndStable && stableLinks && (
            <DownloadButton
              href={stableLinks.windows.primary}
              label={`v${effectiveStable}`}
              channelLabel={t('install.install.stableChannel')}
              variant="outline"
            />
          )}
        </PlatformCard>

        {/* macOS */}
        <PlatformCard
          platform="macos"
          name={t('install.install.macos')}
          subtitle={t('install.install.macosVersion')}
          isDetected={device?.type === 'macos'}
        >
          <DownloadButton
            href={betaLinks.macos.primary}
            label={`v${effectiveStable}`}
            channelLabel={showBetaAndStable ? t('install.install.recommended') : undefined}
          />
          {showBetaAndStable && stableLinks && (
            <DownloadButton
              href={stableLinks.macos.primary}
              label={`v${effectiveStable}`}
              channelLabel={t('install.install.stableChannel')}
              variant="outline"
            />
          )}
        </PlatformCard>

        {/* iOS */}
        <PlatformCard
          platform="ios"
          name={t('install.install.ios')}
          subtitle={t('install.install.iosDevices')}
          isDetected={device?.type === 'ios'}
        >
          {DOWNLOAD_LINKS.ios && (
            <>
              <Button
                variant="outline"
                size="sm"
                className="w-full text-xs"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.ios)}
              >
                <ExternalLink className="w-3.5 h-3.5 mr-1.5 shrink-0" />
                {t('install.install.appStore')}
              </Button>
              <p className="text-[10px] text-muted-foreground">{t('install.install.waymakerNote')}</p>
            </>
          )}
          <p className="text-xs text-muted-foreground mt-1">{t('install.install.kaituMobileComingSoon')}</p>
        </PlatformCard>

        {/* Android */}
        <PlatformCard
          platform="android"
          name={t('install.install.android')}
          subtitle={t('install.install.androidVersion')}
          isDetected={device?.type === 'android'}
        >
          {DOWNLOAD_LINKS.android && (
            <>
              <Button
                variant="outline"
                size="sm"
                className="w-full text-xs"
                onClick={() => openDownloadInNewTab(DOWNLOAD_LINKS.android)}
              >
                <Download className="w-3.5 h-3.5 mr-1.5 shrink-0" />
                {t('install.install.downloadApk')}
              </Button>
              <p className="text-[10px] text-muted-foreground">{t('install.install.waymakerNote')}</p>
            </>
          )}
          <p className="text-xs text-muted-foreground mt-1">{t('install.install.kaituMobileComingSoon')}</p>
        </PlatformCard>
      </div>

      {/* Backup download note + View all releases */}
      <div className="text-center mt-6 space-y-2">
        <p className="text-xs text-muted-foreground">
          {t('install.install.backupDownload')}
          {': '}
          <a
            href={betaLinks.windows.backup}
            target="_blank"
            rel="noopener noreferrer"
            className="hover:text-foreground hover:underline"
          >
            {'Windows'}
          </a>
          {' · '}
          <a
            href={betaLinks.macos.backup}
            target="_blank"
            rel="noopener noreferrer"
            className="hover:text-foreground hover:underline"
          >
            {'macOS'}
          </a>
          {showBetaAndStable && stableLinks && (
            <>
              {' · '}
              <a
                href={stableLinks.windows.backup}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-foreground hover:underline"
              >
                {`Windows v${effectiveStable}`}
              </a>
              {' · '}
              <a
                href={stableLinks.macos.backup}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-foreground hover:underline"
              >
                {`macOS v${effectiveStable}`}
              </a>
            </>
          )}
        </p>
        <Link href="/releases" className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1">
          {t('install.install.viewAllReleases')}
          <ArrowRight className="w-3 h-3" />
        </Link>
      </div>
    </>
  );
}
