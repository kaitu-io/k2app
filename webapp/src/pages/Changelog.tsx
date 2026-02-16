import { useState } from 'react';
import { useTranslation } from 'react-i18next';

const CHANGELOG_URL = 'https://kaitu.io/changelog';

export function Changelog() {
  const { t } = useTranslation('settings');
  const [isLoading, setIsLoading] = useState(true);

  return (
    <div className="relative h-full">
      {/* Progress bar at top during load */}
      {isLoading && (
        <div className="absolute top-0 left-0 right-0 z-10">
          <div className="h-1 bg-primary animate-pulse rounded-full" />
        </div>
      )}

      {/* Loading state */}
      {isLoading && (
        <div className="flex items-center justify-center py-8 bg-[--color-bg-paper]">
          <span className="text-sm text-text-secondary">
            {t('changelog.loading')}
          </span>
        </div>
      )}

      <iframe
        data-testid="changelog-iframe"
        src={CHANGELOG_URL}
        title={t('changelog.title')}
        className="w-full border-none"
        style={{ height: 'calc(100vh - 120px)' }}
        onLoad={() => setIsLoading(false)}
      />
    </div>
  );
}
