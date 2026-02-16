import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../api/cloud';
import type { Member } from '../api/types';

export function MemberManagement() {
  const { t } = useTranslation();
  const [members, setMembers] = useState<Member[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [newEmail, setNewEmail] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Member | null>(null);

  useEffect(() => {
    async function load() {
      setIsLoading(true);
      try {
        const resp = await cloudApi.getMembers();
        setMembers((resp.data as Member[]) || []);
      } catch {
        // silently fail
      } finally {
        setIsLoading(false);
      }
    }
    load();
  }, []);

  const handleAddMember = async () => {
    if (!newEmail.trim()) return;
    try {
      await cloudApi.addMember(newEmail.trim());
      // Refresh the list
      const resp = await cloudApi.getMembers();
      setMembers((resp.data as Member[]) || []);
      setNewEmail('');
    } catch {
      // silently fail
    }
  };

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return;
    await cloudApi.deleteMember(deleteTarget.id);
    setMembers((prev) => prev.filter((m) => m.id !== deleteTarget.id));
    setDeleteTarget(null);
  };

  const getStatusLabel = (status: string): string => {
    switch (status) {
      case 'active':
        return t('members:status_active');
      case 'expired':
        return t('members:status_expired');
      case 'not_activated':
        return t('members:status_not_activated');
      default:
        return status;
    }
  };

  const getStatusStyle = (status: string): string => {
    switch (status) {
      case 'active':
        return 'bg-[--color-success-bg] text-[--color-success]';
      case 'expired':
        return 'bg-[--color-warning-bg] text-[--color-warning]';
      case 'not_activated':
        return 'bg-[--color-info-bg] text-[--color-info]';
      default:
        return 'bg-[--color-glass-bg] text-[--color-text-secondary]';
    }
  };

  const getInitial = (email: string): string => {
    return email.charAt(0).toUpperCase();
  };

  if (isLoading) {
    return (
      <div className="p-4">
        <p>{t('common:loading')}</p>
      </div>
    );
  }

  return (
    <div className="p-4">
      <h1 className="text-xl font-semibold mb-4">{t('members:title')}</h1>

      {/* Add Member */}
      <div className="flex items-center gap-2 mb-4">
        <input
          value={newEmail}
          onChange={(e) => setNewEmail(e.target.value)}
          placeholder={t('members:email_placeholder')}
          className="flex-1 px-3 py-2 rounded border border-[--color-card-border] bg-[--color-bg-default] text-[--color-text-primary]"
        />
        <button
          onClick={handleAddMember}
          className="px-4 py-2 rounded border border-[--color-primary] text-[--color-primary]"
        >
          {t('members:add_member')}
        </button>
      </div>

      {members.length === 0 ? (
        <p>{t('members:no_members')}</p>
      ) : (
        <div className="space-y-3">
          {members.map((member) => (
            <div
              key={member.id}
              data-testid={`member-card-${member.id}`}
              className="rounded-lg p-4 border border-[--color-card-border] bg-[--color-card-bg] flex items-center gap-3"
            >
              {/* Avatar */}
              <div className="bg-[--color-primary] w-9 h-9 rounded-full flex items-center justify-center text-white font-medium text-sm shrink-0">
                {getInitial(member.email)}
              </div>

              {/* Info */}
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{member.email}</p>
              </div>

              {/* Status chip */}
              <span
                className={`text-xs px-2 py-0.5 rounded-full ${getStatusStyle(member.status)}`}
              >
                {getStatusLabel(member.status)}
              </span>

              {/* Delete */}
              <button
                onClick={() => setDeleteTarget(member)}
                className="text-sm text-[--color-error]"
              >
                {t('members:delete')}
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      {deleteTarget && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-[--color-bg-paper] rounded-lg p-6 max-w-sm w-full mx-4">
            <div className="bg-[--color-error-gradient] -m-6 mb-4 p-4 rounded-t-lg">
              <h2 className="text-lg font-semibold text-white">
                {t('members:delete_confirm_title')}
              </h2>
            </div>
            <p className="text-[--color-text-secondary] mb-6">
              {t('members:delete_confirm_message')}
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setDeleteTarget(null)}
                className="px-4 py-2 rounded border border-[--color-card-border] text-[--color-text-primary]"
              >
                {t('members:cancel')}
              </button>
              <button
                onClick={handleDeleteConfirm}
                className="px-4 py-2 rounded bg-[--color-error] text-white"
              >
                {t('members:confirm')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
