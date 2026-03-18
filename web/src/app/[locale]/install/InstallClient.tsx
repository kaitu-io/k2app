"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Tabs, TabsContent } from '@/components/ui/tabs';
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from '@/components/ui/accordion';
import { getDownloadLinks } from '@/lib/constants';
import type { MobileLinks } from '@/lib/downloads';
import { detectDevice, triggerAutoDownload, type DeviceType } from '@/lib/device-detection';
import { PlatformIcon, PLATFORM_COLORS, PLATFORM_IDS, type PlatformId } from './platform-icons';
import { WindowsPanel, MacOSPanel, LinuxPanel, IOSPanel, AndroidPanel } from './platform-panels';
import { ArrowRight } from 'lucide-react';
import { Link } from '@/i18n/routing';

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
  };

  return (
    <div className="grid grid-cols-3 sm:grid-cols-5 gap-2 mb-8">
      {PLATFORM_IDS.map((id) => (
        <button
          key={id}
          onClick={() => onSelect(id)}
          className={`flex flex-col items-center gap-1.5 px-3 py-3 rounded-lg border transition-all ${
            selected === id
              ? 'border-primary bg-primary/10 shadow-sm'
              : 'border-transparent hover:bg-muted/50'
          }`}
        >
          <PlatformIcon type={id} className={`w-8 h-8 ${PLATFORM_COLORS[id]}`} />
          <span className={`text-xs font-medium ${selected === id ? 'text-foreground' : 'text-muted-foreground'}`}>
            {labels[id]}
          </span>
        </button>
      ))}
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
  const [selectedPlatform, setSelectedPlatform] = useState<PlatformId>('windows');
  const [copied, setCopied] = useState(false);

  const displayVersion = betaVersion || serverStable!;
  const isBeta = !!(betaVersion && betaVersion !== serverStable);
  const downloadLinks = getDownloadLinks(displayVersion);
  const stableDownloadLinks = isBeta && serverStable ? getDownloadLinks(serverStable) : null;

  // Device detection -> auto-select tab + auto-download for desktop/android
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const platformParam = params.get('platform') as DeviceType | null;
    const noAutoDownload = params.get('nodownload') !== null;
    const validPlatforms: DeviceType[] = ['windows', 'macos', 'linux', 'ios', 'android'];

    let detectedType: DeviceType;
    if (platformParam && validPlatforms.includes(platformParam)) {
      detectedType = platformParam;
    } else {
      detectedType = detectDevice().type;
    }

    // Map to PlatformId (unknown -> windows fallback)
    if (PLATFORM_IDS.includes(detectedType as PlatformId)) {
      setSelectedPlatform(detectedType as PlatformId);
    }

    // Auto-download for desktop platforms and Android (skip iOS/Linux/unknown)
    if (!noAutoDownload) {
      const autoDownloadLink =
        detectedType === 'windows' ? downloadLinks.windows.primary :
        detectedType === 'macos' ? downloadLinks.macos.primary :
        detectedType === 'android' ? (mobileLinks?.android.primary ?? null) :
        null;

      if (autoDownloadLink) {
        // Small delay so the page renders first, user sees the tab before download starts.
        // Uses hidden iframe to avoid popup blocker (window.open in setTimeout gets blocked).
        const timer = setTimeout(() => triggerAutoDownload(autoDownloadLink), 800);
        return () => clearTimeout(timer);
      }
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const copyCliCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText('curl -fsSL https://kaitu.io/i/k2 | sudo bash');
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
      <PlatformTabBar selected={selectedPlatform} onSelect={setSelectedPlatform} t={t} />

      {/* Tab Content — panels handle their own hero, download button, and install guides */}
      <Tabs value={selectedPlatform} onValueChange={(v) => setSelectedPlatform(v as PlatformId)}>
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
            onCopy={copyCliCommand}
            copied={copied}
          />
        </TabsContent>
        <TabsContent value="linux">
          <LinuxPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            onCopy={copyCliCommand}
            copied={copied}
          />
        </TabsContent>
        <TabsContent value="ios">
          <IOSPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            link={mobileLinks?.ios ?? null}
          />
        </TabsContent>
        <TabsContent value="android">
          <AndroidPanel
            t={t}
            version={displayVersion}
            isBeta={isBeta}
            primaryLink={mobileLinks?.android.primary ?? ''}
            backupLink={mobileLinks?.android.backup ?? ''}
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
