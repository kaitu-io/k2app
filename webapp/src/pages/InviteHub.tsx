import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useInviteStore } from '../stores/invite.store';
import { useUserStore } from '../stores/user.store';
import { getPlatform } from '../platform';
import { ExpirationSelectorPopover } from '../components/ExpirationSelectorPopover';
import { InviteRule } from '../components/InviteRule';
import { RetailerStatsOverview } from '../components/RetailerStatsOverview';

export function InviteHub() {
  const { t } = useTranslation('invite');
  const {
    latestCode,
    codes,
    isLoading,
    loadLatest,
    generateCode,
    updateRemark,
    loadAllCodes,
  } = useInviteStore();
  const { user } = useUserStore();
  const platform = getPlatform();

  const [showExpiration, setShowExpiration] = useState(false);
  const [selectedExpiration, setSelectedExpiration] = useState('24h');
  const [isEditingRemark, setIsEditingRemark] = useState(false);
  const [remarkDraft, setRemarkDraft] = useState('');

  const isRetailer = user?.role === 'retailer';

  useEffect(() => {
    loadLatest();
    if (isRetailer) {
      loadAllCodes();
    }
  }, [loadLatest, loadAllCodes, isRetailer]);

  const handleCopyCode = async () => {
    if (latestCode) {
      await platform.writeClipboard(latestCode.code);
    }
  };

  const handleGenerate = async () => {
    await generateCode();
  };

  const handleEditRemark = () => {
    setRemarkDraft(latestCode?.remark ?? '');
    setIsEditingRemark(true);
  };

  const handleSaveRemark = async () => {
    if (latestCode) {
      await updateRemark(latestCode.id, remarkDraft);
      setIsEditingRemark(false);
    }
  };

  const handleShareClick = () => {
    setShowExpiration(true);
  };

  const handleExpirationSelect = (value: string) => {
    setSelectedExpiration(value);
  };

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('title')}</h1>

      {/* Invite Code Display */}
      <div className="bg-[--color-card-bg] rounded-lg p-4 space-y-3">
        <p className="text-sm text-[--color-text-secondary]">{t('inviteCode')}</p>
        <div
          data-testid="invite-code-display"
          className="font-mono text-2xl tracking-wider text-[--color-accent] cursor-pointer select-all text-center py-2"
          onClick={handleCopyCode}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') handleCopyCode();
          }}
        >
          {latestCode?.code ?? '------'}
        </div>

        {/* Stats */}
        {latestCode && (
          <div className="flex justify-center gap-6 text-sm">
            <div className="text-center">
              <span data-testid="stat-registered" className="font-semibold text-[--color-success-text]">
                {latestCode.registeredCount ?? 0}
              </span>
              <span className="ml-1 text-[--color-text-secondary]">{t('stats.registered')}</span>
            </div>
            <div className="text-center">
              <span data-testid="stat-purchased" className="font-semibold text-[--color-info-text]">
                {latestCode.purchasedCount ?? 0}
              </span>
              <span className="ml-1 text-[--color-text-secondary]">{t('stats.purchased')}</span>
            </div>
          </div>
        )}

        {/* Remark */}
        <div className="flex items-center gap-2">
          {isEditingRemark ? (
            <>
              <input
                data-testid="remark-input"
                type="text"
                value={remarkDraft}
                onChange={(e) => setRemarkDraft(e.target.value)}
                className="flex-1 text-xs border rounded px-2 py-1"
                placeholder={t('remarkPlaceholder')}
              />
              <button
                data-testid="save-remark-button"
                className="text-xs text-[--color-primary]"
                onClick={handleSaveRemark}
              >
                {t('saveRemark')}
              </button>
            </>
          ) : (
            <>
              <span className="text-xs text-[--color-text-secondary]">
                {latestCode?.remark || t('remarkPlaceholder')}
              </span>
              <button
                data-testid="edit-remark-button"
                className="text-xs text-[--color-primary]"
                onClick={handleEditRemark}
              >
                {t('editRemark')}
              </button>
            </>
          )}
        </div>
      </div>

      {/* QR Code Section */}
      {!platform.isMobile && latestCode && (
        <div data-testid="qr-code-section" className="bg-[--color-card-bg] rounded-lg p-4 flex flex-col items-center gap-3">
          <p className="text-sm font-medium">{t('qrCode')}</p>
          <div className="w-40 h-40 bg-gray-100 rounded flex items-center justify-center text-xs text-gray-400">
            {/* QR code placeholder - real QR library integration deferred */}
            QR: {latestCode.code}
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex gap-3">
        <button
          data-testid="share-button"
          className="flex-1 py-2 px-4 rounded-lg bg-[--color-primary] text-white text-sm font-medium"
          onClick={handleShareClick}
        >
          {t('share')}
        </button>
        <button
          data-testid="generate-button"
          className="flex-1 py-2 px-4 rounded-lg border-dashed border-2 border-[--color-primary] text-[--color-primary] text-sm font-medium"
          onClick={handleGenerate}
          disabled={isLoading}
        >
          {isLoading ? t('generating') : t('generateNew')}
        </button>
      </div>

      {/* Expiration Selector */}
      {showExpiration && (
        <ExpirationSelectorPopover
          open={showExpiration}
          selected={selectedExpiration}
          onSelect={handleExpirationSelect}
          onClose={() => setShowExpiration(false)}
        />
      )}

      {/* Retailer Stats or Invite Rules */}
      {isRetailer ? (
        <RetailerStatsOverview codes={codes} />
      ) : (
        <InviteRule />
      )}
    </div>
  );
}
