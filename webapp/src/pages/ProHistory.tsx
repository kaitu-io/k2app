import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../api/cloud';
import { Pagit } from '../components/Pagit';
import type { ProHistory as ProHistoryType } from '../api/types';

const PAGE_SIZE = 10;

export function ProHistory() {
  const { t } = useTranslation();
  const [histories, setHistories] = useState<ProHistoryType[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [currentPage, setCurrentPage] = useState(1);

  useEffect(() => {
    async function load() {
      setIsLoading(true);
      try {
        const resp = await cloudApi.getProHistories();
        setHistories((resp.data as ProHistoryType[]) || []);
      } catch {
        // silently fail
      } finally {
        setIsLoading(false);
      }
    }
    load();
  }, []);

  const totalPages = Math.ceil(histories.length / PAGE_SIZE);
  const paginatedItems = histories.slice(
    (currentPage - 1) * PAGE_SIZE,
    currentPage * PAGE_SIZE
  );

  const getStatusLabel = (status: string): string => {
    switch (status) {
      case 'active':
        return t('prohistory:status_active');
      case 'expired':
        return t('prohistory:status_expired');
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
      default:
        return 'bg-[--color-glass-bg] text-[--color-text-secondary]';
    }
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
      <h1 className="text-xl font-semibold mb-4">{t('prohistory:title')}</h1>

      {histories.length === 0 ? (
        <p>{t('prohistory:no_history')}</p>
      ) : (
        <>
          <div className="space-y-3">
            {paginatedItems.map((item) => (
              <div
                key={item.id}
                data-testid={`history-card-${item.id}`}
                className="rounded-lg p-4 border border-[--color-card-border] bg-[--color-card-bg]"
              >
                <div className="flex items-center justify-between">
                  <div>
                    <p className="font-medium text-sm">{item.planName}</p>
                    <p className="text-xs text-[--color-text-secondary] mt-1">
                      {item.startAt.split('T')[0]} - {item.endAt.split('T')[0]}
                    </p>
                  </div>
                  <span
                    className={`text-xs px-2 py-0.5 rounded-full ${getStatusStyle(item.status)}`}
                  >
                    {getStatusLabel(item.status)}
                  </span>
                </div>
              </div>
            ))}
          </div>

          <Pagit
            currentPage={currentPage}
            totalPages={totalPages}
            onPageChange={setCurrentPage}
          />
        </>
      )}
    </div>
  );
}
