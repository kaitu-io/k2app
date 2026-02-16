import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { usePurchaseStore } from '../stores/purchase.store';
import { useAuthStore } from '../stores/auth.store';
import { EmailLoginForm } from '../components/EmailLoginForm';
import type { Plan } from '../api/types';

function sortPlansByPeriod(plans: Plan[]): Plan[] {
  return [...plans].sort((a, b) => {
    const periodA = parseInt(a.period, 10) || 0;
    const periodB = parseInt(b.period, 10) || 0;
    return periodA - periodB;
  });
}

function getHighlightedPlanId(plans: Plan[]): string | null {
  if (plans.length === 0) return null;
  // Highlight the plan with the longest period (best value)
  let best: Plan = plans[0]!;
  for (const plan of plans) {
    if ((parseInt(plan.period, 10) || 0) > (parseInt(best.period, 10) || 0)) {
      best = plan;
    }
  }
  return best.id;
}

export function Purchase() {
  const { t } = useTranslation('purchase');
  const {
    plans,
    selectedPlanId,
    orderPreview,
    currentOrder,
    isLoading,
    loadPlans,
    selectPlan,
    setCampaignCode,
    previewOrder,
    createOrder,
  } = usePurchaseStore();
  const { isLoggedIn } = useAuthStore();

  const [campaignInput, setCampaignInput] = useState('');
  const [showPaymentResult, setShowPaymentResult] = useState(false);

  useEffect(() => {
    loadPlans();
  }, [loadPlans]);

  // Show payment result when order is created
  useEffect(() => {
    if (currentOrder) {
      setShowPaymentResult(true);
    }
  }, [currentOrder]);

  const sortedPlans = sortPlansByPeriod(plans);
  const highlightedId = getHighlightedPlanId(plans);

  const handleApplyCampaign = () => {
    setCampaignCode(campaignInput);
    previewOrder();
  };

  const handlePay = async () => {
    await createOrder();
  };

  const handlePaymentClose = () => {
    setShowPaymentResult(false);
  };

  // Unauthenticated: show inline login
  if (!isLoggedIn) {
    return (
      <div className="p-4 space-y-6">
        <h1 className="text-xl font-semibold">{t('title')}</h1>
        <div className="space-y-4">
          <h2 className="text-lg font-medium">{t('loginRequired')}</h2>
          <p className="text-sm text-[--color-text-disabled]">{t('loginRequiredMessage')}</p>
          <EmailLoginForm />
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('title')}</h1>

      {/* Plan List */}
      <div className="space-y-3">
        <h2 className="text-lg font-medium">{t('selectPlan')}</h2>
        {sortedPlans.map((plan) => {
          const isSelected = selectedPlanId === plan.id;
          const isHighlighted = highlightedId === plan.id;

          return (
            <div
              key={plan.id}
              data-testid="plan-card"
              onClick={() => selectPlan(plan.id)}
              className={`relative p-4 rounded-lg border cursor-pointer transition-colors ${
                isSelected
                  ? 'border-[--color-selected-border]'
                  : 'border-[--color-card-border]'
              }`}
            >
              {isHighlighted && (
                <span className="absolute top-2 right-2 text-xs px-2 py-0.5 rounded-full bg-[--color-success-bg] text-[--color-success] font-medium">
                  {t('recommended')}
                </span>
              )}
              <div className="font-medium text-[--color-text-primary]">{plan.name}</div>
              <div className="text-sm text-[--color-text-disabled] mt-1">{plan.description}</div>
              <div className="mt-2 text-lg font-bold text-[--color-text-primary]">
                {plan.price}
              </div>
            </div>
          );
        })}
      </div>

      {/* Campaign Code */}
      <div className="space-y-2">
        <label className="text-sm font-medium">{t('campaignCode')}</label>
        <div className="flex gap-2">
          <input
            type="text"
            value={campaignInput}
            onChange={(e) => setCampaignInput(e.target.value)}
            placeholder={t('campaignCodePlaceholder')}
            className="flex-1 rounded-lg border border-[--color-card-border] bg-transparent px-3 py-2 text-sm outline-none"
          />
          <button
            onClick={handleApplyCampaign}
            className="px-4 py-2 rounded-lg bg-[--color-primary] text-white text-sm font-medium"
          >
            {t('applyCampaignCode')}
          </button>
        </div>
      </div>

      {/* Order Preview */}
      {orderPreview && (
        <div className="p-4 rounded-lg border border-[--color-card-border] space-y-2">
          <h3 className="font-medium">{t('orderSummary')}</h3>
          <div className="flex justify-between text-sm">
            <span>{t('orderTotal')}</span>
            <span className="text-[--color-text-primary] font-bold">{orderPreview.total}</span>
          </div>
          {orderPreview.discount && (
            <div className="flex justify-between text-sm">
              <span>{t('discount')}</span>
              <span className="text-[--color-success]">-{orderPreview.discount}</span>
            </div>
          )}
        </div>
      )}

      {/* Pay Button */}
      <button
        onClick={handlePay}
        disabled={!selectedPlanId || isLoading}
        className="w-full py-3.5 rounded-lg bg-[--color-primary] text-white font-bold disabled:opacity-50"
      >
        {t('payNow')}
      </button>

      {/* Payment Result Dialog */}
      {showPaymentResult && currentOrder && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-xl p-6 max-w-sm w-full mx-4 space-y-4">
            <h2 className="text-lg font-semibold">{t('paymentResult')}</h2>
            <div>
              {currentOrder.status === 'paid' && (
                <>
                  <h3 className="font-medium text-[--color-success]">{t('paymentSuccess')}</h3>
                  <p className="text-sm text-[--color-text-disabled] mt-1">{t('paymentSuccessMessage')}</p>
                </>
              )}
              {currentOrder.status === 'pending' && (
                <>
                  <h3 className="font-medium">{t('paymentPending')}</h3>
                  <p className="text-sm text-[--color-text-disabled] mt-1">{t('paymentPendingMessage')}</p>
                </>
              )}
              {currentOrder.status === 'failed' && (
                <>
                  <h3 className="font-medium text-red-600">{t('paymentFailed')}</h3>
                  <p className="text-sm text-[--color-text-disabled] mt-1">{t('paymentFailedMessage')}</p>
                </>
              )}
            </div>
            <button
              onClick={handlePaymentClose}
              className="w-full py-2.5 rounded-lg border border-[--color-card-border] text-sm font-medium"
            >
              {t('close')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
