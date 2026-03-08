"use client";

import { useEffect, useState, Suspense } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import {
  Package,
  ChevronDown,
  ChevronUp,
  Calendar,
  Download,
  Monitor,
  Apple,
} from 'lucide-react';

interface VersionData {
  version: string;
  date: string;
  content: string;
  sections: {
    newFeatures: string[];
    bugFixes: string[];
    improvements: string[];
    breakingChanges: string[];
  };
  channel: 'beta' | 'stable';
  hasDownloads: boolean;
  downloads: {
    windows?: string;
    macos?: string;
    windowsBackup?: string;
    macosBackup?: string;
  };
}

interface ReleasesData {
  generated: string;
  latestBeta: string | null;
  latestStable: string | null;
  versions: VersionData[];
}

/**
 * Renders changelog item text with basic markdown bold support.
 * Only replaces **bold** patterns with <strong>; no user input involved.
 * Content comes from our own releases.json, not external sources.
 */
function renderItemText(text: string): string {
  return text.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
}

function ChannelBadge({ channel }: { channel: 'beta' | 'stable' }) {
  const t = useTranslations();
  if (channel === 'beta') {
    return (
      <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-500/15 text-blue-400">
        {t('releases.betaTag')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-500/15 text-green-400">
      {t('releases.stableTag')}
    </span>
  );
}

function DownloadAssets({ downloads }: { downloads: VersionData['downloads'] }) {
  const t = useTranslations();
  return (
    <div className="flex flex-wrap gap-3 mt-4 pt-4 border-t border-border">
      <span className="text-sm font-medium text-muted-foreground self-center">
        {t('releases.downloadAssets')}:
      </span>
      {downloads.windows && (
        <div className="flex items-center gap-2">
          <a
            href={downloads.windows}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-muted hover:bg-muted/80 text-sm text-foreground transition-colors"
          >
            <Monitor className="w-4 h-4" />
            {t('releases.downloadWindows')}
          </a>
          {downloads.windowsBackup && (
            <a
              href={downloads.windowsBackup}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground hover:text-foreground hover:underline"
            >
              {t('releases.backupDownload')}
            </a>
          )}
        </div>
      )}
      {downloads.macos && (
        <div className="flex items-center gap-2">
          <a
            href={downloads.macos}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-muted hover:bg-muted/80 text-sm text-foreground transition-colors"
          >
            <Apple className="w-4 h-4" />
            {t('releases.downloadMacOS')}
          </a>
          {downloads.macosBackup && (
            <a
              href={downloads.macosBackup}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground hover:text-foreground hover:underline"
            >
              {t('releases.backupDownload')}
            </a>
          )}
        </div>
      )}
    </div>
  );
}

function SectionList({
  items,
  colorClass,
  label,
  bullet,
}: {
  items: string[];
  colorClass: string;
  label: string;
  bullet: string;
}) {
  if (items.length === 0) return null;
  return (
    <div>
      <h4 className={`text-sm font-semibold ${colorClass} mb-2`}>{label}</h4>
      <ul className="space-y-1.5">
        {items.map((item, idx) => (
          <li key={idx} className="text-sm text-foreground/80 flex items-start">
            <span className="mr-2">{bullet}</span>
            {/* Content is from our own releases.json, not user input */}
            {/* eslint-disable-next-line react/no-danger */}
            <span dangerouslySetInnerHTML={{ __html: renderItemText(item) }} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function ReleaseCard({
  version,
  isExpanded,
  onToggle,
}: {
  version: VersionData;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const t = useTranslations();
  const bullet = t('releases.bullet');

  const hasSections =
    version.sections.newFeatures.length > 0 ||
    version.sections.bugFixes.length > 0 ||
    version.sections.improvements.length > 0 ||
    version.sections.breakingChanges.length > 0;

  return (
    <div className="bg-card rounded-lg shadow-md overflow-hidden border border-border">
      {/* Header */}
      <div
        className="p-6 cursor-pointer hover:bg-muted/50 transition-colors"
        onClick={onToggle}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-3 mb-2 flex-wrap">
              <div className="flex items-center gap-2">
                <Package className="w-5 h-5 text-secondary" />
                <h3 className="text-xl font-bold text-foreground">
                  v{version.version}
                </h3>
              </div>
              <ChannelBadge channel={version.channel} />
              <div className="flex items-center gap-1 text-sm text-muted-foreground">
                <Calendar className="w-4 h-4" />
                <span>{version.date}</span>
              </div>
            </div>

            {/* Summary badges */}
            <div className="flex flex-wrap gap-2">
              {version.sections.newFeatures.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
                  {`${version.sections.newFeatures.length} ${t('releases.sections.newFeatures')}`}
                </span>
              )}
              {version.sections.bugFixes.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-500/15 text-red-400">
                  {`${version.sections.bugFixes.length} ${t('releases.sections.bugFixes')}`}
                </span>
              )}
              {version.sections.improvements.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-secondary/15 text-secondary">
                  {`${version.sections.improvements.length} ${t('releases.sections.improvements')}`}
                </span>
              )}
              {version.sections.breakingChanges.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-amber-400/15 text-amber-400">
                  {`${version.sections.breakingChanges.length} ${t('releases.sections.breakingChanges')}`}
                </span>
              )}
            </div>
          </div>

          <div className="ml-4 flex items-center gap-2">
            {version.hasDownloads && (
              <Download className="w-4 h-4 text-muted-foreground" />
            )}
            {isExpanded ? (
              <ChevronUp className="w-5 h-5 text-muted-foreground" />
            ) : (
              <ChevronDown className="w-5 h-5 text-muted-foreground" />
            )}
          </div>
        </div>
      </div>

      {/* Expanded content */}
      {isExpanded && (
        <div className="px-6 pb-6 border-t border-border">
          {hasSections && (
            <div className="mt-4 space-y-4">
              <SectionList
                items={version.sections.newFeatures}
                colorClass="text-primary"
                label={t('releases.sections.newFeatures')}
                bullet={bullet}

              />
              <SectionList
                items={version.sections.bugFixes}
                colorClass="text-red-400"
                label={t('releases.sections.bugFixes')}
                bullet={bullet}

              />
              <SectionList
                items={version.sections.improvements}
                colorClass="text-secondary"
                label={t('releases.sections.improvements')}
                bullet={bullet}

              />
              <SectionList
                items={version.sections.breakingChanges}
                colorClass="text-amber-400"
                label={t('releases.sections.breakingChanges')}
                bullet={bullet}

              />
            </div>
          )}

          {/* Download assets */}
          {version.hasDownloads && (
            <DownloadAssets downloads={version.downloads} />
          )}
        </div>
      )}
    </div>
  );
}

function ReleasesClientInner() {
  const t = useTranslations();
  const { isEmbedded, showNavigation, showFooter, compactLayout } = useEmbedMode();
  const [data, setData] = useState<ReleasesData | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedVersions, setExpandedVersions] = useState<Set<string>>(new Set());

  useEffect(() => {
    fetch('/releases.json')
      .then(res => res.json())
      .then((jsonData: ReleasesData) => {
        setData(jsonData);
        // Expand latest version by default
        if (jsonData.versions.length > 0) {
          setExpandedVersions(new Set([jsonData.versions[0].version]));
        }
        setLoading(false);
      })
      .catch(() => {
        setLoading(false);
      });
  }, []);

  const toggleVersion = (version: string) => {
    setExpandedVersions(prev => {
      const next = new Set(prev);
      if (next.has(version)) {
        next.delete(version);
      } else {
        next.add(version);
      }
      return next;
    });
  };

  const expandAll = () => {
    if (data) {
      setExpandedVersions(new Set(data.versions.map(v => v.version)));
    }
  };

  const collapseAll = () => {
    setExpandedVersions(new Set());
  };

  return (
    <div className={`min-h-screen bg-background ${isEmbedded ? 'embedded-mode' : ''}`}>
      {showNavigation && <Header />}

      {/* Hero Section */}
      <section className={compactLayout ? 'py-6 px-4 sm:px-6 lg:px-8' : 'py-16 px-4 sm:px-6 lg:px-8'}>
        <div className="max-w-4xl mx-auto text-center">
          <div className={`flex items-center justify-center ${compactLayout ? 'mb-3' : 'mb-6'}`}>
            <div className={`${compactLayout ? 'p-2' : 'p-3'} bg-secondary/15 rounded-full`}>
              <Package className={`${compactLayout ? 'w-6 h-6' : 'w-8 h-8'} text-secondary`} />
            </div>
          </div>
          <h1 className={`${compactLayout ? 'text-2xl sm:text-3xl' : 'text-4xl sm:text-5xl'} font-bold text-foreground mb-4`}>
            {t('releases.title')}
          </h1>
          <p className={`${compactLayout ? 'text-base' : 'text-xl'} text-muted-foreground ${compactLayout ? 'mb-4' : 'mb-8'} max-w-3xl mx-auto`}>
            {t('releases.subtitle')}
          </p>
        </div>
      </section>

      {/* Main Content */}
      <div className={`max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 ${compactLayout ? 'py-4' : 'py-12'}`}>
        {loading ? (
          <div className="text-center py-12">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-secondary mx-auto" />
          </div>
        ) : data && data.versions.length > 0 ? (
          <>
            {/* Controls */}
            <div className="flex justify-end gap-2 mb-6">
              <Button variant="outline" size="sm" onClick={expandAll}>
                {t('releases.controls.expandAll')}
              </Button>
              <Button variant="outline" size="sm" onClick={collapseAll}>
                {t('releases.controls.collapseAll')}
              </Button>
            </div>

            {/* Version list */}
            <div className="space-y-4">
              {data.versions.map(version => (
                <ReleaseCard
                  key={version.version}
                  version={version}
                  isExpanded={expandedVersions.has(version.version)}
                  onToggle={() => toggleVersion(version.version)}
                />
              ))}
            </div>
          </>
        ) : (
          <div className="text-center py-12 text-muted-foreground">
            {t('releases.noData')}
          </div>
        )}
      </div>

      {/* CTA Section */}
      {showFooter && (
        <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
          <div className="max-w-4xl mx-auto text-center">
            <h2 className="text-2xl font-bold text-foreground mb-4">
              {t('releases.latestRelease')}
            </h2>
            <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
              <Link href="/install">
                <Button size="lg">
                  <Download className="w-4 h-4 mr-2" />
                  {t('releases.downloadAssets')}
                </Button>
              </Link>
            </div>
          </div>
        </section>
      )}

      {showFooter && <Footer />}
    </div>
  );
}

export default function ReleasesClient() {
  return (
    <Suspense
      fallback={
        <div className="min-h-screen bg-background flex items-center justify-center">
          <div className="text-center">
            <div className="w-8 h-8 border-4 border-secondary border-t-transparent rounded-full animate-spin mx-auto mb-4" />
          </div>
        </div>
      }
    >
      <ReleasesClientInner />
    </Suspense>
  );
}
