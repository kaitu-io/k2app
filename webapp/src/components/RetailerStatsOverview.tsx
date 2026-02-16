import { useTranslation } from 'react-i18next';
import type { InviteCode } from '../api/types';

interface RetailerStatsOverviewProps {
  codes: InviteCode[];
}

export function RetailerStatsOverview({ codes }: RetailerStatsOverviewProps) {
  const { t } = useTranslation('invite');

  const totalInvites = codes.length;
  const registeredCount = codes.reduce((sum, c) => sum + (c.registeredCount ?? 0), 0);
  const purchasedCount = codes.reduce((sum, c) => sum + (c.purchasedCount ?? 0), 0);

  return (
    <div data-testid="retailer-stats-overview" className="bg-[--color-card-bg] rounded-lg p-4 space-y-4">
      <h3 className="text-sm font-medium">{t('retailerStats.title')}</h3>
      <div className="grid grid-cols-3 gap-4">
        <div className="text-center">
          <p className="text-2xl font-semibold text-[--color-info-text]">{totalInvites}</p>
          <p className="text-xs text-[--color-text-secondary]">{t('retailerStats.totalInvites')}</p>
        </div>
        <div className="text-center">
          <p className="text-2xl font-semibold text-[--color-success-text]">{registeredCount}</p>
          <p className="text-xs text-[--color-text-secondary]">{t('retailerStats.registered')}</p>
        </div>
        <div className="text-center">
          <p className="text-2xl font-semibold text-[--color-success-text]">{purchasedCount}</p>
          <p className="text-xs text-[--color-text-secondary]">{t('retailerStats.purchased')}</p>
        </div>
      </div>
    </div>
  );
}
