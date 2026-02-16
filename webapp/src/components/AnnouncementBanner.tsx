import { useUiStore } from '../stores/ui.store';

export function AnnouncementBanner() {
  const { appConfig } = useUiStore();

  if (!appConfig?.announcement) return null;

  return (
    <div className="bg-info-bg border-b border-info-border px-4 py-2 text-sm text-text-primary">
      {appConfig.announcement}
    </div>
  );
}
