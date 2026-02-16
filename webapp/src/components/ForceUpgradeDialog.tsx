import { useTranslation } from 'react-i18next';
import { useUiStore } from '../stores/ui.store';

function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map(Number);
  const pb = b.split('.').map(Number);
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const na = pa[i] ?? 0;
    const nb = pb[i] ?? 0;
    if (na < nb) return -1;
    if (na > nb) return 1;
  }
  return 0;
}

export function ForceUpgradeDialog({ currentVersion }: { currentVersion: string }) {
  const { t } = useTranslation();
  const { appConfig } = useUiStore();

  if (!appConfig?.minClientVersion) return null;
  if (compareVersions(currentVersion, appConfig.minClientVersion) >= 0) return null;

  return (
    <div className="fixed inset-0 bg-black/80 z-[100] flex items-center justify-center">
      <div className="w-[calc(100%-32px)] max-w-sm rounded-xl bg-bg-paper p-6 text-center space-y-4">
        <h2 className="text-lg font-bold text-text-primary">
          {t('common:force_upgrade_title', 'Update Required')}
        </h2>
        <p className="text-sm text-text-secondary">
          {t('common:force_upgrade_message', 'Please update to continue')}
        </p>
        {appConfig.downloadUrl && (
          <a
            href={appConfig.downloadUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-block w-full rounded-lg bg-primary text-white font-bold py-3"
          >
            {t('common:download', 'Download')}
          </a>
        )}
      </div>
    </div>
  );
}
