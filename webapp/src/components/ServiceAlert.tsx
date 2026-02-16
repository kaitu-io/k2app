import { useTranslation } from 'react-i18next';
import { useVpnStore } from '../stores/vpn.store';

export function ServiceAlert() {
  const { t } = useTranslation();
  const { daemonReachable } = useVpnStore();

  if (daemonReachable !== false) return null;

  return (
    <div className="bg-error-bg border border-error-border rounded-lg mx-4 my-2 px-4 py-3 text-sm text-error">
      {t('common:service_unavailable', 'Service Unavailable')}
    </div>
  );
}
