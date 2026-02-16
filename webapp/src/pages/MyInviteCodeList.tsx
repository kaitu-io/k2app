import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useInviteStore } from '../stores/invite.store';
import type { InviteCode } from '../api/types';

interface InviteCodeCardProps {
  inviteCode: InviteCode;
  onSaveRemark: (id: string, remark: string) => void;
  t: (key: string) => string;
}

function InviteCodeCard({ inviteCode, onSaveRemark, t }: InviteCodeCardProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [remarkValue, setRemarkValue] = useState(inviteCode.remark);

  const handleSave = () => {
    onSaveRemark(inviteCode.id, remarkValue);
    setIsEditing(false);
  };

  return (
    <div className="space-y-2">
      {/* Code value */}
      <div className="font-mono text-accent text-lg">
        {inviteCode.code}
      </div>

      {/* Stats row */}
      <div className="flex items-center gap-3 text-sm">
        <span className={inviteCode.used ? 'text-text-disabled' : 'text-success'}>
          {inviteCode.used ? t('invite.used') : t('invite.unused')}
        </span>
        {inviteCode.usedBy && (
          <span className="text-text-secondary">{inviteCode.usedBy}</span>
        )}
      </div>

      {/* Remark */}
      <div className="flex items-center gap-2 text-sm">
        {isEditing ? (
          <>
            <input
              type="text"
              value={remarkValue}
              onChange={(e) => setRemarkValue(e.target.value)}
              className="flex-1 rounded border border-card-border bg-bg-paper text-text-primary px-2 py-1 text-sm outline-none"
            />
            <button
              onClick={handleSave}
              className="text-primary text-sm font-medium"
            >
              {t('invite.saveRemark')}
            </button>
          </>
        ) : (
          <>
            <span className="text-text-secondary">
              {inviteCode.remark || t('invite.noRemark')}
            </span>
            <button
              onClick={() => setIsEditing(true)}
              className="text-primary text-sm font-medium"
            >
              {t('invite.editRemark')}
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export function MyInviteCodeList() {
  const { t } = useTranslation('invite');
  const { codes, isLoading, error, loadAllCodes, updateRemark } = useInviteStore();

  useEffect(() => {
    loadAllCodes();
  }, [loadAllCodes]);

  const handleSaveRemark = (id: string, remark: string) => {
    updateRemark(id, remark);
  };

  if (isLoading) {
    return (
      <div className="p-4 flex items-center justify-center">
        <span className="text-sm text-text-secondary">
          {t('invite.loading')}
        </span>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('invite.myCodesTitle')}</h1>

      {error && (
        <div className="p-3 rounded-lg bg-error-bg text-error text-sm">
          {error}
        </div>
      )}

      {codes.length === 0 ? (
        <div className="text-center text-text-secondary py-8">
          {t('invite.noCodes')}
        </div>
      ) : (
        <div className="space-y-0">
          {codes.map((inviteCode, index) => (
            <div key={inviteCode.id}>
              <InviteCodeCard
                inviteCode={inviteCode}
                onSaveRemark={handleSaveRemark}
                t={t}
              />
              {index < codes.length - 1 && (
                <div className="border-b border-divider my-4" />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
