import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../api/cloud';
import type { Member } from '../api/types';

interface MemberSelectionProps {
  onSelect: (memberId: string) => void;
  currentUserId?: string;
}

export function MemberSelection({ onSelect, currentUserId }: MemberSelectionProps) {
  const { t } = useTranslation('purchase');
  const [members, setMembers] = useState<Member[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const resp = await cloudApi.getMembers();
        if (!cancelled) {
          setMembers((resp.data ?? []) as Member[]);
        }
      } catch {
        // ignore
      } finally {
        if (!cancelled) {
          setIsLoading(false);
        }
      }
    }
    load();
    return () => { cancelled = true; };
  }, []);

  if (isLoading) {
    return <div>{t('loading')}</div>;
  }

  return (
    <div className="space-y-2">
      {currentUserId && (
        <button
          onClick={() => onSelect(currentUserId)}
          className="w-full text-left px-3 py-2 rounded-lg border border-[--color-card-border] hover:bg-[--color-hover-bg] text-sm"
        >
          {t('buyForSelf')}
        </button>
      )}

      {members.length === 0 && !currentUserId ? (
        <div className="text-sm text-[--color-text-disabled] py-2">{t('noMembers')}</div>
      ) : (
        members.map((member) => (
          <button
            key={member.id}
            onClick={() => onSelect(member.id)}
            className="w-full text-left px-3 py-2 rounded-lg border border-[--color-card-border] hover:bg-[--color-hover-bg] text-sm"
          >
            {member.email}
          </button>
        ))
      )}

      {members.length === 0 && currentUserId && (
        <div className="text-sm text-[--color-text-disabled] py-2">{t('noMembers')}</div>
      )}
    </div>
  );
}
