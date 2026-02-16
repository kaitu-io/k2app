import { useTranslation } from 'react-i18next';

const DOWNLOAD_LINKS: Record<string, string> = {
  windows: 'https://kaitu.io/download/windows',
  macos: 'https://kaitu.io/download/macos',
  android: 'https://kaitu.io/download/android',
  ios: 'https://kaitu.io/download/ios',
};

const PLATFORM_ICONS: Record<string, string> = {
  windows: '\u{1F5A5}',
  macos: '\u{1F4BB}',
  android: '\u{1F4F1}',
  ios: '\u{1F34F}',
};

interface PlatformCardProps {
  platform: string;
  label: string;
  downloadLabel: string;
}

function PlatformCard({ platform, label, downloadLabel }: PlatformCardProps) {
  return (
    <div className="rounded-xl bg-[--color-card-bg] border border-card-border p-4 flex items-center justify-between">
      <div className="flex items-center gap-3">
        <span className="text-2xl" role="img" aria-label={platform}>
          {PLATFORM_ICONS[platform]}
        </span>
        <span className="text-text-primary font-medium">{label}</span>
      </div>
      <a
        href={DOWNLOAD_LINKS[platform]}
        target="_blank"
        rel="noopener noreferrer"
        className="rounded-lg bg-primary text-white px-4 py-2 text-sm font-medium"
      >
        {downloadLabel}
      </a>
    </div>
  );
}

export function DeviceInstall() {
  const { t } = useTranslation('settings');

  const platforms = [
    { key: 'windows', label: t('deviceInstall.windows') },
    { key: 'macos', label: t('deviceInstall.macos') },
    { key: 'android', label: t('deviceInstall.android') },
    { key: 'ios', label: t('deviceInstall.ios') },
  ];

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('deviceInstall.title')}</h1>

      <div className="space-y-3">
        {platforms.map(({ key, label }) => (
          <PlatformCard
            key={key}
            platform={key}
            label={label}
            downloadLabel={t('deviceInstall.download')}
          />
        ))}
      </div>

      {/* QR Code Section */}
      <div className="flex flex-col items-center gap-3 pt-4">
        <p className="text-sm text-text-secondary">
          {t('deviceInstall.scanQr')}
        </p>
        <div
          data-testid="qr-container"
          className="bg-white rounded-xl p-4 flex items-center justify-center"
          style={{ width: 160, height: 160 }}
        >
          {/* QR code placeholder - in production, use a QR library */}
          <div className="w-full h-full bg-white border border-card-border rounded flex items-center justify-center text-xs text-text-disabled">
            QR
          </div>
        </div>
      </div>
    </div>
  );
}
