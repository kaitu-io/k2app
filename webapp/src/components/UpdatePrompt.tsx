import { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { getVpnClient } from '../vpn-client';
import type { UpdateCheckResult } from '../vpn-client';

function formatSize(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function getPlatform(): 'ios' | 'android' | 'web' {
  const cap = (window as any)?.Capacitor;
  if (cap?.getPlatform) return cap.getPlatform();
  return 'web';
}

export function UpdatePrompt() {
  const { t } = useTranslation();
  const isNativeApp = typeof window !== 'undefined' &&
    ((window as any).__TAURI__ || (window as any).Capacitor);
  const [update, setUpdate] = useState<UpdateCheckResult | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [progress, setProgress] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    const client = getVpnClient();
    if (!client.checkForUpdates) return;
    client.checkForUpdates().then((result) => {
      if (result.type !== 'none') setUpdate(result);
    });
  }, []);

  const handleUpdate = useCallback(async () => {
    if (!update) return;
    const client = getVpnClient();
    setError(null);

    try {
      if (update.type === 'native') {
        const platform = getPlatform();
        if (platform === 'ios') {
          await client.installNativeUpdate?.({ path: '' });
        } else {
          setDownloading(true);
          const unsub = client.onDownloadProgress?.((percent) => setProgress(percent));
          try {
            const result = await client.downloadNativeUpdate?.();
            if (result) await client.installNativeUpdate?.({ path: result.path });
          } finally {
            unsub?.();
          }
        }
      } else if (update.type === 'web') {
        setDownloading(true);
        await client.applyWebUpdate?.();
        setApplied(true);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    } finally {
      setDownloading(false);
      setProgress(null);
    }
  }, [update]);

  if (!isNativeApp) return null;

  if (!update || update.type === 'none' || dismissed) return null;

  const platform = getPlatform();
  const isIos = platform === 'ios';
  const isNative = update.type === 'native';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white dark:bg-gray-800 rounded-2xl p-6 mx-4 max-w-sm w-full shadow-xl">
        <h2 className="text-lg font-semibold mb-2">{t('updateAvailable')}</h2>

        {update.version && (
          <p className="text-sm text-gray-600 dark:text-gray-400">
            {t('updateVersion', { version: update.version })}
          </p>
        )}
        {update.size && (
          <p className="text-sm text-gray-600 dark:text-gray-400">
            {t('updateSize', { size: formatSize(update.size) })}
          </p>
        )}

        {downloading && (
          <div className="mt-3">
            <p className="text-sm text-blue-600">{t('updateDownloading')}</p>
            {progress !== null && (
              <div className="mt-1 w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                <div
                  className="bg-blue-600 h-2 rounded-full transition-all"
                  style={{ width: `${progress}%` }}
                  role="progressbar"
                  aria-valuenow={progress}
                  aria-valuemin={0}
                  aria-valuemax={100}
                />
              </div>
            )}
          </div>
        )}

        {error && (
          <p className="mt-3 text-sm text-red-600">
            {t('updateFailed', { message: error })}
          </p>
        )}

        {applied && (
          <p className="mt-3 text-sm text-green-600">
            {t('updateRestartToApply')}
          </p>
        )}

        <div className="mt-4 flex gap-3">
          <button
            className="flex-1 px-4 py-2 text-sm rounded-lg bg-gray-200 dark:bg-gray-700"
            onClick={() => setDismissed(true)}
            disabled={downloading}
          >
            {t('updateLater')}
          </button>
          {!applied && (
            <button
              className="flex-1 px-4 py-2 text-sm rounded-lg bg-blue-600 text-white"
              onClick={handleUpdate}
              disabled={downloading}
            >
              {isNative && isIos ? t('updateGoToAppStore') : t('updateNow')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
