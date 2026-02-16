import { useState, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

interface VersionItemProps {
  version: string;
}

const DEV_MODE_TAP_COUNT = 5;
const DEV_MODE_TAP_WINDOW_MS = 3000;

export function VersionItem({ version }: VersionItemProps) {
  const { t } = useTranslation('account');
  const [devMode, setDevMode] = useState(false);
  const tapTimestamps = useRef<number[]>([]);

  const handleVersionClick = useCallback(() => {
    const now = Date.now();
    tapTimestamps.current = [
      ...tapTimestamps.current.filter((ts) => now - ts < DEV_MODE_TAP_WINDOW_MS),
      now,
    ];

    if (tapTimestamps.current.length >= DEV_MODE_TAP_COUNT) {
      setDevMode(true);
      tapTimestamps.current = [];
    }
  }, []);

  return (
    <div className="px-4 py-6 flex flex-col items-center gap-1">
      <span
        className="text-xs text-[--color-text-disabled] cursor-pointer select-none"
        onClick={handleVersionClick}
      >
        {version}
      </span>
      {devMode && (
        <span className="text-xs text-[--color-primary]">
          {t('devModeActivated')}
        </span>
      )}
    </div>
  );
}
