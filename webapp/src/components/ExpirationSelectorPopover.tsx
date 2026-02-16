import { useTranslation } from 'react-i18next';

const EXPIRATION_OPTIONS = [
  { value: '1h', labelKey: 'expirationOptions.1h' },
  { value: '24h', labelKey: 'expirationOptions.24h' },
  { value: '7d', labelKey: 'expirationOptions.7d' },
  { value: '30d', labelKey: 'expirationOptions.30d' },
  { value: 'never', labelKey: 'expirationOptions.never' },
] as const;

interface ExpirationSelectorPopoverProps {
  open: boolean;
  selected?: string;
  onSelect: (value: string) => void;
  onClose: () => void;
}

export function ExpirationSelectorPopover({
  open,
  selected,
  onSelect,
  onClose,
}: ExpirationSelectorPopoverProps) {
  const { t } = useTranslation('invite');

  if (!open) return null;

  const handleSelect = (value: string) => {
    onSelect(value);
    onClose();
  };

  return (
    <div data-testid="expiration-popover" className="bg-[--color-card-bg] rounded-lg shadow-lg p-3 space-y-1">
      <p className="text-sm font-medium mb-2">{t('expiration')}</p>
      {EXPIRATION_OPTIONS.map((option) => (
        <button
          key={option.value}
          data-testid={`expiration-option-${option.value}`}
          className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
            selected === option.value
              ? 'bg-[--color-primary] text-white'
              : 'hover:bg-gray-100'
          }`}
          onClick={() => handleSelect(option.value)}
        >
          {t(option.labelKey)}
        </button>
      ))}
    </div>
  );
}
