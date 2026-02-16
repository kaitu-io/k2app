import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../api/cloud';
import { getVpnClient } from '../vpn-client';
import type { Device } from '../api/types';

export function Devices() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<Device[]>([]);
  const [currentUdid, setCurrentUdid] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [editingDeviceId, setEditingDeviceId] = useState<string | null>(null);
  const [editRemark, setEditRemark] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Device | null>(null);

  useEffect(() => {
    async function load() {
      setIsLoading(true);
      try {
        const [devicesResp, udid] = await Promise.all([
          cloudApi.getDevices(),
          getVpnClient().getUDID(),
        ]);
        setDevices((devicesResp.data as Device[]) || []);
        setCurrentUdid(udid);
      } catch {
        // silently fail
      } finally {
        setIsLoading(false);
      }
    }
    load();
  }, []);

  const handleEditStart = (device: Device) => {
    setEditingDeviceId(device.id);
    setEditRemark(device.remark);
  };

  const handleEditSave = async (deviceId: string) => {
    await cloudApi.updateDeviceRemark(deviceId, editRemark);
    setDevices((prev) =>
      prev.map((d) => (d.id === deviceId ? { ...d, remark: editRemark } : d))
    );
    setEditingDeviceId(null);
  };

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return;
    await cloudApi.deleteDevice(deleteTarget.id);
    setDevices((prev) => prev.filter((d) => d.id !== deleteTarget.id));
    setDeleteTarget(null);
  };

  if (isLoading) {
    return (
      <div className="p-4">
        <p>{t('common:loading')}</p>
      </div>
    );
  }

  if (devices.length === 0) {
    return (
      <div className="p-4">
        <h1 className="text-xl font-semibold mb-4">{t('devices:title')}</h1>
        <p>{t('devices:no_devices')}</p>
      </div>
    );
  }

  return (
    <div className="p-4">
      <h1 className="text-xl font-semibold mb-4">{t('devices:title')}</h1>

      <div className="space-y-3">
        {devices.map((device) => {
          const isCurrent = device.id === currentUdid;
          const isEditing = editingDeviceId === device.id;

          return (
            <div
              key={device.id}
              data-testid={`device-card-${device.id}`}
              className={`rounded-lg p-4 border ${
                isCurrent
                  ? 'border-[--color-primary] bg-[--color-selected-bg]'
                  : 'border-[--color-card-border] bg-[--color-card-bg]'
              }`}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{device.name}</span>
                  {isCurrent && (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-[--color-primary] text-white">
                      {t('devices:current_device')}
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {!isEditing && (
                    <button
                      onClick={() => handleEditStart(device)}
                      className="text-sm text-[--color-primary]"
                    >
                      {t('devices:edit')}
                    </button>
                  )}
                  <button
                    onClick={() => setDeleteTarget(device)}
                    className="text-sm text-[--color-error]"
                  >
                    {t('devices:delete')}
                  </button>
                </div>
              </div>

              {isEditing ? (
                <div className="mt-2 flex items-center gap-2">
                  <input
                    value={editRemark}
                    onChange={(e) => setEditRemark(e.target.value)}
                    placeholder={t('devices:remark_placeholder')}
                    className="flex-1 px-2 py-1 rounded border border-[--color-card-border] bg-[--color-bg-default] text-[--color-text-primary] text-sm"
                  />
                  <button
                    onClick={() => handleEditSave(device.id)}
                    className="text-sm px-3 py-1 rounded bg-[--color-primary] text-white"
                  >
                    {t('devices:save')}
                  </button>
                </div>
              ) : (
                device.remark && (
                  <p className="text-xs text-[--color-text-secondary] mt-1">
                    {device.remark}
                  </p>
                )
              )}
            </div>
          );
        })}
      </div>

      {/* Delete Confirmation Dialog */}
      {deleteTarget && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-[--color-bg-paper] rounded-lg p-6 max-w-sm w-full mx-4">
            <div className="bg-[--color-error-gradient] -m-6 mb-4 p-4 rounded-t-lg">
              <h2 className="text-lg font-semibold text-white">
                {t('devices:delete_confirm_title')}
              </h2>
            </div>
            <p className="text-[--color-text-secondary] mb-6">
              {t('devices:delete_confirm_message')}
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setDeleteTarget(null)}
                className="px-4 py-2 rounded border border-[--color-card-border] text-[--color-text-primary]"
              >
                {t('devices:cancel')}
              </button>
              <button
                onClick={handleDeleteConfirm}
                className="px-4 py-2 rounded bg-[--color-error] text-white"
              >
                {t('devices:confirm')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
