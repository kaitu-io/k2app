"use client";

import { useEffect, useState, Suspense } from 'react';
import { useTranslations } from 'next-intl';
import NextLink from 'next/link';
import { Button } from '@/components/ui/button';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import { FileText, ChevronDown, ChevronUp, Calendar, Package } from 'lucide-react';

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
}

interface ChangelogData {
  generated: string;
  versions: VersionData[];
}

function VersionCard({ version, isExpanded, onToggle }: {
  version: VersionData;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const t = useTranslations();

  const hasSections =
    version.sections.newFeatures.length > 0 ||
    version.sections.bugFixes.length > 0 ||
    version.sections.improvements.length > 0 ||
    version.sections.breakingChanges.length > 0;

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow-md overflow-hidden border border-gray-200 dark:border-gray-700">
      {/* Header */}
      <div
        className="p-6 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors"
        onClick={onToggle}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-3 mb-2">
              <div className="flex items-center gap-2">
                <Package className="w-5 h-5 text-blue-600" />
                <h3 className="text-xl font-bold text-gray-900 dark:text-white">
                  {`v${version.version}`}
                </h3>
              </div>
              <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                <Calendar className="w-4 h-4" />
                <span>{version.date}</span>
              </div>
            </div>

            {/* Summary badges */}
            <div className="flex flex-wrap gap-2">
              {version.sections.newFeatures.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
                  {`${version.sections.newFeatures.length} ${t('changelog.sections.newFeatures')}`}
                </span>
              )}
              {version.sections.bugFixes.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200">
                  {`${version.sections.bugFixes.length} ${t('changelog.sections.bugFixes')}`}
                </span>
              )}
              {version.sections.improvements.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">
                  {`${version.sections.improvements.length} ${t('changelog.sections.improvements')}`}
                </span>
              )}
              {version.sections.breakingChanges.length > 0 && (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200">
                  {`${version.sections.breakingChanges.length} ${t('changelog.sections.breakingChanges')}`}
                </span>
              )}
            </div>
          </div>

          <div className="ml-4">
            {isExpanded ? (
              <ChevronUp className="w-5 h-5 text-gray-400" />
            ) : (
              <ChevronDown className="w-5 h-5 text-gray-400" />
            )}
          </div>
        </div>
      </div>

      {/* Expanded content */}
      {isExpanded && hasSections && (
        <div className="px-6 pb-6 border-t border-gray-200 dark:border-gray-700">
          <div className="mt-4 space-y-4">
            {version.sections.newFeatures.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold text-green-600 dark:text-green-400 mb-2">
                  {t('changelog.sections.newFeatures')}
                </h4>
                <ul className="space-y-1.5">
                  {version.sections.newFeatures.map((feature, idx) => (
                    <li key={idx} className="text-sm text-gray-700 dark:text-gray-300 flex items-start">
                      <span className="mr-2">{t('changelog.bullet')}</span>
                      <span dangerouslySetInnerHTML={{
                        __html: feature.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
                      }} />
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {version.sections.bugFixes.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold text-red-600 dark:text-red-400 mb-2">
                  {t('changelog.sections.bugFixes')}
                </h4>
                <ul className="space-y-1.5">
                  {version.sections.bugFixes.map((fix, idx) => (
                    <li key={idx} className="text-sm text-gray-700 dark:text-gray-300 flex items-start">
                      <span className="mr-2">{t('changelog.bullet')}</span>
                      <span>{fix}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {version.sections.improvements.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold text-blue-600 dark:text-blue-400 mb-2">
                  {t('changelog.sections.improvements')}
                </h4>
                <ul className="space-y-1.5">
                  {version.sections.improvements.map((improvement, idx) => (
                    <li key={idx} className="text-sm text-gray-700 dark:text-gray-300 flex items-start">
                      <span className="mr-2">{t('changelog.bullet')}</span>
                      <span>{improvement}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {version.sections.breakingChanges.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold text-orange-600 dark:text-orange-400 mb-2">
                  {t('changelog.sections.breakingChanges')}
                </h4>
                <ul className="space-y-1.5">
                  {version.sections.breakingChanges.map((change, idx) => (
                    <li key={idx} className="text-sm text-gray-700 dark:text-gray-300 flex items-start">
                      <span className="mr-2">{t('changelog.bullet')}</span>
                      <span>{change}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function ChangelogContent() {
  const t = useTranslations();
  const { isEmbedded, showNavigation, showFooter, compactLayout } = useEmbedMode();
  const [data, setData] = useState<ChangelogData | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedVersions, setExpandedVersions] = useState<Set<string>>(new Set(['0.3.18'])); // Latest expanded by default

  useEffect(() => {
    fetch('/changelog.json')
      .then(res => res.json())
      .then(jsonData => {
        setData(jsonData);
        setLoading(false);
      })
      .catch(() => {
        setLoading(false);
      });
  }, []);

  const toggleVersion = (version: string) => {
    setExpandedVersions(prev => {
      const newSet = new Set(prev);
      if (newSet.has(version)) {
        newSet.delete(version);
      } else {
        newSet.add(version);
      }
      return newSet;
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
    <div className={`min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 ${isEmbedded ? 'embedded-mode' : ''}`}>
      {showNavigation && <Header />}

      {/* Hero Section */}
      <section className={compactLayout ? "py-6 px-4 sm:px-6 lg:px-8" : "py-16 px-4 sm:px-6 lg:px-8"}>
        <div className="max-w-4xl mx-auto text-center">
          <div className={`flex items-center justify-center ${compactLayout ? 'mb-3' : 'mb-6'}`}>
            <div className={`${compactLayout ? 'p-2' : 'p-3'} bg-blue-100 dark:bg-blue-900 rounded-full`}>
              <FileText className={`${compactLayout ? 'w-6 h-6' : 'w-8 h-8'} text-blue-600`} />
            </div>
          </div>
          <h1 className={`${compactLayout ? 'text-2xl sm:text-3xl' : 'text-4xl sm:text-5xl'} font-bold text-gray-900 dark:text-white mb-4`}>
            {t('changelog.title')}
          </h1>
          <p className={`${compactLayout ? 'text-base' : 'text-xl'} text-gray-600 dark:text-gray-300 ${compactLayout ? 'mb-4' : 'mb-8'} max-w-3xl mx-auto`}>
            {t('changelog.subtitle')}
          </p>
        </div>
      </section>

      {/* Main Content */}
      <div className={`max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 ${compactLayout ? 'py-4' : 'py-12'}`}>
        {loading ? (
          <div className="text-center py-12">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
          </div>
        ) : data && data.versions.length > 0 ? (
          <>
            {/* Controls */}
            <div className="flex justify-end gap-2 mb-6">
              <Button
                variant="outline"
                size="sm"
                onClick={expandAll}
              >
                {t('changelog.controls.expandAll')}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={collapseAll}
              >
                {t('changelog.controls.collapseAll')}
              </Button>
            </div>

            {/* Version list */}
            <div className="space-y-4">
              {data.versions.map(version => (
                <VersionCard
                  key={version.version}
                  version={version}
                  isExpanded={expandedVersions.has(version.version)}
                  onToggle={() => toggleVersion(version.version)}
                />
              ))}
            </div>
          </>
        ) : (
          <div className="text-center py-12 text-gray-500 dark:text-gray-400">
            {t('changelog.noData')}
          </div>
        )}
      </div>

      {/* CTA Section - Hidden in embedded mode */}
      {showFooter && (
        <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
          <div className="max-w-4xl mx-auto text-center">
            <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-4">
              {t('changelog.cta.title')}
            </h2>
            <p className="text-gray-600 dark:text-gray-300 mb-8">
              {t('changelog.cta.description')}
            </p>
            <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
              <NextLink href="/install">
                <Button size="lg">
                  {t('changelog.cta.download')}
                </Button>
              </NextLink>
              <NextLink href="/">
                <Button variant="outline" size="lg">
                  {t('hero.routers.backToHome')}
                </Button>
              </NextLink>
            </div>
          </div>
        </section>
      )}

      {showFooter && <Footer />}
    </div>
  );
}

export default function ChangelogPage() {
  return (
    <Suspense fallback={
      <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 flex items-center justify-center">
        <div className="text-center">
          <div className="w-8 h-8 border-4 border-blue-600 border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
        </div>
      </div>
    }>
      <ChangelogContent />
    </Suspense>
  );
}
