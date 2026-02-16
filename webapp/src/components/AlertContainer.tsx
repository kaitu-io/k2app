import { useUiStore } from '../stores/ui.store';

export function AlertContainer() {
  const { alerts, removeAlert } = useUiStore();

  if (!alerts || alerts.length === 0) return null;

  return (
    <div className="fixed top-4 right-4 z-[200] space-y-2 max-w-sm w-full">
      {alerts.map((alert) => (
        <div
          key={alert.id}
          className={`rounded-lg px-4 py-3 text-sm text-text-primary shadow-lg ${
            alert.type === 'error'
              ? 'bg-error-bg border border-error-border'
              : alert.type === 'warning'
              ? 'bg-warning-bg border border-warning-border'
              : alert.type === 'success'
              ? 'bg-success-bg border border-success-border'
              : 'bg-info-bg border border-info-border'
          }`}
        >
          {alert.message}
        </div>
      ))}
    </div>
  );
}
