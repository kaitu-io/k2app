"use client";

import { useEffect, useState, useCallback, useRef } from 'react';
import { useTranslations } from 'next-intl';
import { Tabs, TabsContent } from '@/components/ui/tabs';
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from '@/components/ui/accordion';
import { getDownloadLinks } from '@/lib/constants';
import { siteBrand } from '@/lib/brands';
import type { MobileLinks } from '@/lib/downloads';
import { detectDevice, triggerAutoDownload, type DeviceType } from '@/lib/device-detection';
import { PlatformIcon, PLATFORM_COLORS, PLATFORM_IDS, type PlatformId } from './platform-icons';
import { WindowsPanel, MacOSPanel, LinuxPanel, IOSPanel, AndroidPanel } from './platform-panels';
import { ArrowRight } from 'lucide-react';
import { Link, useRouter } from '@/i18n/routing';
import { track } from '@/lib/funnel';

const AUTO_DOWNLOAD_SECONDS = 5;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
  mobileLinks?: MobileLinks | null;
}

// ---------------------------------------------------------------------------
// FAQ
// ---------------------------------------------------------------------------

const FAQ_ITEMS = ['edgeBlock', 'chromeBlock', 'windowsSmartScreen', 'macosGatekeeper', 'androidUsbInstall', 'security'] as const;

function getDefaultFaqItem(platform: string): string | undefined {
  switch (platform) {
    case 'macos': return 'macosGatekeeper';
    case 'windows': return 'windowsSmartScreen';
    case 'android': return 'androidInstallBlock';
    default: return undefined;
  }
}

// ---------------------------------------------------------------------------
// PlatformTabBar — custom grid tab selector (3 cols mobile, 5 cols desktop)
// ---------------------------------------------------------------------------

function PlatformTabBar({
  selected,
  onSelect,
  t,
}: {
  selected: PlatformId;
  onSelect: (id: PlatformId) => void;
  t: (key: string) => string;
}) {
  const labels: Record<PlatformId, string> = {
    windows: t('install.install.windows'),
    macos: t('install.install.macos'),
    linux: t('install.install.linux'),
    ios: t('install.install.ios'),
    android: t('install.install.android'),
    router: t('install.install.router'),
  };

  const tileClass = (isSelected: boolean) =>
    `flex flex-col items-center gap-1.5 px-3 py-3 rounded-lg border transition-all ${
      isSelected
        ? 'border-primary bg-primary/10 shadow-sm'
        : 'border-transparent hover:bg-muted/50'
    }`;

  return (
    <div className="grid grid-cols-3 sm:grid-cols-6 gap-2 mb-8">
      {PLATFORM_IDS.map((id) => {
        if (id === 'router') {
          return (
            <Link
              key={id}
              href="/routers"
              target="_blank"
              rel="noopener noreferrer"
              className={tileClass(false)}
            >
              <PlatformIcon type={id} className={`w-8 h-8 ${PLATFORM_COLORS[id]}`} />
              <span className="text-xs font-medium text-muted-foreground">
                {labels[id]}
              </span>
            </Link>
          );
        }
        return (
          <button
            key={id}
            onClick={() => onSelect(id)}
            className={tileClass(selected === id)}
          >
            <PlatformIcon type={id} className={`w-8 h-8 ${PLATFORM_COLORS[id]}`} />
            <span className={`text-xs font-medium ${selected === id ? 'text-foreground' : 'text-muted-foreground'}`}>
              {labels[id]}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// FAQ JSON-LD — structured data for GEO (AI search optimization)
// ---------------------------------------------------------------------------

function FaqJsonLd({ t }: { t: (key: string) => string }) {
  const jsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: FAQ_ITEMS.map((item) => ({
      '@type': 'Question',
      name: t(`install.install.faq.${item}.question`),
      acceptedAnswer: {
        '@type': 'Answer',
        text: t(`install.install.faq.${item}.answer`),
      },
    })),
  };

  return (
    <script
      type="application/ld+json"
      suppressHydrationWarning
      dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
    />
  );
}

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export default function InstallClient({ betaVersion, stableVersion: serverStable, mobileLinks }: InstallClientProps) {
  const t = useTranslations();
  const router = useRouter();
  const [selectedPlatform, setSelectedPlatform] = useState<PlatformId>('windows');
  const [copied, setCopied] = useState(false);
  const [countdown, setCountdown] = useState<number | null>(null);
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const autoDownloadLinkRef = useRef<string | null>(null);
  // Platform of the pending automatic download — reported with the event.
  const autoDownloadPlatformRef = useRef<PlatformId | null>(null);

  const displayVersion = betaVersion || serverStable!;
  const isBeta = !!(betaVersion && betaVersion !== serverStable);
  const downloadLinks = getDownloadLinks(displayVersion);
  const stableDownloadLinks = isBeta && serverStable ? getDownloadLinks(serverStable) : null;

  const cancelAutoDownload = useCallback(() => {
    if (countdownRef.current) {
      clearInterval(countdownRef.current);
      countdownRef.current = null;
    }
    setCountdown(null);
    autoDownloadLinkRef.current = null;
    autoDownloadPlatformRef.current = null;
  }, []);

  // Cancel auto-download when user switches platform tab
  const handlePlatformSelect = useCallback((id: PlatformId) => {
    cancelAutoDownload();
    setSelectedPlatform(id);
  }, [cancelAutoDownload]);

  // Device detection -> auto-select tab + auto-download for desktop/android
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const platformParam = params.get('platform');
    const noAutoDownload = params.get('nodownload') !== null;
    const validPlatforms: PlatformId[] = ['windows', 'macos', 'linux', 'ios', 'android'];

    if (platformParam === 'router') {
      router.replace('/routers');
      return;
    }

    // Only a visit that actually renders the install page counts as a view.
    track('install_view');

    let selectedId: PlatformId | null = null;
    let detectedType: DeviceType;
    if (platformParam && (validPlatforms as string[]).includes(platformParam)) {
      selectedId = platformParam as PlatformId;
      detectedType = platformParam as DeviceType;
    } else {
      detectedType = detectDevice().type;
      if (PLATFORM_IDS.includes(detectedType as PlatformId)) {
        selectedId = detectedType as PlatformId;
      }
    }

    if (selectedId) {
      setSelectedPlatform(selectedId);
    }

    // Auto-download for desktop platforms and Android (skip iOS/Linux/unknown)
    if (!noAutoDownload) {
      const link =
        detectedType === 'windows' ? downloadLinks.windows.primary :
        detectedType === 'macos' ? downloadLinks.macos.primary :
        detectedType === 'android' ? (mobileLinks?.android.primary ?? null) :
        null;

      if (link) {
        autoDownloadLinkRef.current = link;
        autoDownloadPlatformRef.current = detectedType as PlatformId;
        setCountdown(AUTO_DOWNLOAD_SECONDS);
        countdownRef.current = setInterval(() => {
          setCountdown((prev) => {
            if (prev === null || prev <= 1) {
              clearInterval(countdownRef.current!);
              countdownRef.current = null;
              // The link ref is cleared as it is consumed, so a state updater
              // React invokes twice still downloads — and counts — only once.
              if (autoDownloadLinkRef.current) {
                triggerAutoDownload(autoDownloadLinkRef.current);
                // The automatic download is the main desktop conversion: count
                // it as an install click, marked `auto`, only when it really fires.
                track('install_click', { plan: autoDownloadPlatformRef.current ?? undefined, source: 'auto' });
                autoDownloadLinkRef.current = null;
                autoDownloadPlatformRef.current = null;
              }
              return null;
            }
            return prev - 1;
          });
        }, 1000);
      }
    }

    return () => {
      if (countdownRef.current) {
        clearInterval(countdownRef.current);
        countdownRef.current = null;
      }
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // The CLI block appears on the Linux tab (its only install path) and on the
  // macOS tab; the copy counts as an install click for the tab it came from.
  const copyCliCommand = useCallback(async (platform: 'linux' | 'macos') => {
    track('install_click', { plan: platform, source: 'cli' });
    try {
      await navigator.clipboard.writeText(`curl -fsSL ${siteBrand().baseUrl}/i/k2 | sudo bash`);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API unavailable
    }
  }, []);

  // -------------------------------------------------------------------------
  // Render
  // -------------------------------------------------------------------------

  return (
    <>
      {/* Platform Tab Bar */}
      <PlatformTabBar selected={selectedPlatform} onSelect={handlePlatformSelect} t={t} />

      {/* Auto-download countdown */}
      {countdown !== null && (
        <div className="flex items-center justify-center gap-3 mb-6 text-sm text-muted-foreground">
          <span>{t('install.install.autoDownloadCountdown', { seconds: countdown })}</span>
          <button
            onClick={cancelAutoDownload}
            className="text-xs text-muted-foreground/70 hover:text-foreground underline underline-offset-2 transition-colors"
          >
            {t('install.install.cancelAutoDownload')}
          </button>
        </div>
      )}

      {/* Tab Content — panels handle their own hero, download button, and install guides */}
      <Tabs value={selectedPlatform} onValueChange={(v) => handlePlatformSelect(v as PlatformId)}>
        <TabsContent value="windows">
          <WindowsPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            primaryLink={downloadLinks.windows.primary}
            backupLink={downloadLinks.windows.backup}
          />
        </TabsContent>
        <TabsContent value="macos">
          <MacOSPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            primaryLink={downloadLinks.macos.primary}
            backupLink={downloadLinks.macos.backup}
            onCopy={() => copyCliCommand('macos')}
            copied={copied}
          />
        </TabsContent>
        <TabsContent value="linux">
          <LinuxPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            onCopy={() => copyCliCommand('linux')}
            copied={copied}
          />
        </TabsContent>
        <TabsContent value="ios">
          <IOSPanel
            t={t}
            version={mobileLinks?.ios?.version ?? null}
            link={mobileLinks?.ios?.url ?? null}
          />
        </TabsContent>
        <TabsContent value="android">
          <AndroidPanel
            t={t}
            version={mobileLinks?.android?.version ?? null}
            primaryLink={mobileLinks?.android?.primary ?? ''}
            backupLink={mobileLinks?.android?.backup ?? ''}
          />
        </TabsContent>
      </Tabs>

      {/* Stable version alternative */}
      {isBeta && stableDownloadLinks && (
        <p className="text-xs text-muted-foreground text-center mt-6">
          {t('install.install.alsoAvailableStable', { version: serverStable! })}
          {': '}
          <a href={stableDownloadLinks.windows.primary} target="_blank" rel="noopener noreferrer" className="hover:text-foreground hover:underline">{'Windows'}</a>
          {' \u00B7 '}
          <a href={stableDownloadLinks.macos.primary} target="_blank" rel="noopener noreferrer" className="hover:text-foreground hover:underline">{'macOS'}</a>
        </p>
      )}

      {/* View all releases */}
      <div className="text-center mt-4">
        <Link href="/releases" className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1">
          {t('install.install.viewAllReleases')}
          <ArrowRight className="w-3 h-3" />
        </Link>
      </div>

      {/* FAQ Section */}
      <div className="mt-12">
        <h3 className="text-lg font-semibold text-foreground mb-4">
          {t('install.install.needHelp')}
        </h3>
        <Accordion type="single" collapsible defaultValue={getDefaultFaqItem(selectedPlatform)}>
          {FAQ_ITEMS.map((item) => (
            <AccordionItem key={item} value={item}>
              <AccordionTrigger>
                {t(`install.install.faq.${item}.question`)}
              </AccordionTrigger>
              <AccordionContent>
                <p className="text-muted-foreground">
                  {t(`install.install.faq.${item}.answer`)}
                </p>
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>

        {/* FAQPage JSON-LD structured data for GEO */}
        <FaqJsonLd t={t} />
      </div>
    </>
  );
}
