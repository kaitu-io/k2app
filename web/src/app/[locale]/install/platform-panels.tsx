/* eslint-disable react/jsx-no-literals */
"use client";

import { Button } from '@/components/ui/button';
import { Download, ExternalLink } from 'lucide-react';
import { PlatformIcon, PLATFORM_COLORS } from './platform-icons';
import {
  BrowserBlockedGuide,
  SmartScreenGuide,
  MacOSAllowGuide,
  DesktopUsbInstallGuide,
  CliBlock,
  DownloadTipCard,
} from './install-guides';
import { openDownloadInNewTab } from '@/lib/device-detection';

// ---------------------------------------------------------------------------
// Shared prop interfaces
// ---------------------------------------------------------------------------

interface PlatformPanelProps {
  t: (key: string, params?: Record<string, string | number>) => string;
  version: string;
  isBeta: boolean;
}

interface DesktopPanelProps extends PlatformPanelProps {
  primaryLink: string;
  backupLink: string;
}

// ---------------------------------------------------------------------------
// Shared layout fragments
// ---------------------------------------------------------------------------

function PanelHeroIcon({ type }: { type: string }) {
  return (
    <div className="bg-primary/10 rounded-2xl p-3 w-16 h-16 mx-auto mb-6 flex items-center justify-center">
      <PlatformIcon type={type} className={`w-10 h-10 ${PLATFORM_COLORS[type]}`} />
    </div>
  );
}

function VersionLabel({ t, version, isBeta }: PlatformPanelProps) {
  return (
    <p className="text-sm text-muted-foreground mb-6">
      {t('install.install.latestVersion', { version })}
      {isBeta && (
        <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
          {t('install.install.beta')}
        </span>
      )}
    </p>
  );
}

function BackupLink({ href, t }: { href: string; t: (key: string) => string }) {
  return (
    <div className="mt-2">
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        {t('install.install.backupDownload')}
      </a>
    </div>
  );
}

// ---------------------------------------------------------------------------
// WindowsPanel
// ---------------------------------------------------------------------------

export function WindowsPanel({
  t,
  version,
  isBeta,
  primaryLink,
  backupLink,
}: DesktopPanelProps) {
  const filename = `Kaitu_${version}_x64.exe`;
  const publisher = 'ALL NATION CONNECT TECHNOLOGY PTE. LTD.';

  return (
    <div className="text-center">
      <PanelHeroIcon type="windows" />

      <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
        {t('install.install.heroTitle.windows')}
      </h1>
      <VersionLabel t={t} version={version} isBeta={isBeta} />

      <Button size="lg" onClick={() => openDownloadInNewTab(primaryLink)}>
        <Download className="w-5 h-5 mr-2" />
        {t('install.install.downloadButton')} v{version}
      </Button>
      <BackupLink href={backupLink} t={t} />

      {/* Install guides */}
      <div className="mt-8 max-w-xl mx-auto space-y-4 text-left">
        <DownloadTipCard title={t('install.install.faq.edgeBlock.question')}>
          <BrowserBlockedGuide filename={filename} browser="edge" />
        </DownloadTipCard>
        <DownloadTipCard title={t('install.install.faq.chromeBlock.question')}>
          <BrowserBlockedGuide filename={filename} browser="chrome" />
        </DownloadTipCard>
        <DownloadTipCard title={t('install.install.faq.windowsSmartScreen.question')}>
          <SmartScreenGuide filename={filename} publisher={publisher} />
        </DownloadTipCard>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// MacOSPanel
// ---------------------------------------------------------------------------

export function MacOSPanel({
  t,
  version,
  isBeta,
  primaryLink,
  backupLink,
  onCopy,
  copied,
}: DesktopPanelProps & { onCopy: () => void; copied: boolean }) {
  const filename = `Kaitu_${version}_universal.pkg`;
  const publisher = 'ALL NATION CONNECT TECHNOLOGY PTE. LTD.';

  return (
    <div className="text-center">
      <PanelHeroIcon type="macos" />

      <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
        {t('install.install.heroTitle.macos')}
      </h1>
      <VersionLabel t={t} version={version} isBeta={isBeta} />

      <Button size="lg" onClick={() => openDownloadInNewTab(primaryLink)}>
        <Download className="w-5 h-5 mr-2" />
        {t('install.install.downloadButton')} v{version}
      </Button>
      <BackupLink href={backupLink} t={t} />

      {/* CLI block */}
      <div className="max-w-lg mx-auto mt-6">
        <p className="text-xs text-muted-foreground mb-2">
          {t('install.install.terminalInstall')}
        </p>
        <CliBlock onCopy={onCopy} copied={copied} />
      </div>

      {/* Install guides */}
      <div className="mt-8 max-w-xl mx-auto space-y-4 text-left">
        <DownloadTipCard title={t('install.install.faq.chromeBlock.question')}>
          <BrowserBlockedGuide filename={filename} browser="chrome" />
        </DownloadTipCard>
        <DownloadTipCard title={t('install.install.faq.macosGatekeeper.question')}>
          <MacOSAllowGuide publisher={publisher} />
        </DownloadTipCard>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// LinuxPanel
// ---------------------------------------------------------------------------

export function LinuxPanel({
  t,
  version,
  isBeta,
  onCopy,
  copied,
}: PlatformPanelProps & { onCopy: () => void; copied: boolean }) {
  return (
    <div className="text-center">
      <PanelHeroIcon type="linux" />

      <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
        {t('install.install.heroTitle.linux')}
      </h1>
      <VersionLabel t={t} version={version} isBeta={isBeta} />

      {/* CLI as primary action */}
      <div className="max-w-lg mx-auto">
        <CliBlock onCopy={onCopy} copied={copied} />
      </div>

      <p className="text-xs text-muted-foreground mt-3">
        {t('install.install.linuxVersion')} &middot; webkit2gtk-4.1
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// IOSPanel
// ---------------------------------------------------------------------------

export function IOSPanel({
  t,
  version,
  isBeta,
  link,
}: PlatformPanelProps & { link: string | null }) {
  return (
    <div className="text-center">
      <PanelHeroIcon type="ios" />

      <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
        {t('install.install.heroTitle.ios')}
      </h1>
      <VersionLabel t={t} version={version} isBeta={isBeta} />

      {link && (
        <Button size="lg" onClick={() => openDownloadInNewTab(link)}>
          <ExternalLink className="w-5 h-5 mr-2" />
          App Store
        </Button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// AndroidPanel
// ---------------------------------------------------------------------------

export function AndroidPanel({
  t,
  version,
  isBeta,
  primaryLink,
  backupLink,
}: PlatformPanelProps & { primaryLink: string; backupLink: string }) {
  return (
    <div className="text-center">
      <PanelHeroIcon type="android" />

      <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
        {t('install.install.heroTitle.android')}
      </h1>
      <VersionLabel t={t} version={version} isBeta={isBeta} />

      <Button size="lg" onClick={() => openDownloadInNewTab(primaryLink)}>
        <Download className="w-5 h-5 mr-2" />
        {t('install.install.downloadButton')} v{version}
      </Button>
      <BackupLink href={backupLink} t={t} />

      {/* Install guide */}
      <div className="mt-8 max-w-xl mx-auto space-y-4 text-left">
        <DownloadTipCard title={t('install.install.faq.androidUsbInstall.question')}>
          <DesktopUsbInstallGuide />
        </DownloadTipCard>
      </div>
    </div>
  );
}
