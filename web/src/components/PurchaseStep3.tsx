"use client";

import { useTranslations } from "next-intl";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import {
  LoaderIcon,
  PlusIcon,
  AlertTriangleIcon,
  CreditCardIcon,
  ShieldIcon,
  ZapIcon,
  HeadphonesIcon
} from "lucide-react";
import type { Plan, Order } from "@/lib/api";

export interface PurchaseStep3Props {
  // Plan and pricing data
  plans: Plan[];
  selectedPlan: string;
  orderData: Order | null;

  // Campaign code state
  showCampaign: boolean;
  campaignCode: string;
  campaignError: string;
  onCampaignToggle: (show: boolean) => void;
  onCampaignCodeChange: (code: string) => void;
  onCampaignErrorClear: () => void;

  // Selection state
  selectedForMyself: boolean;
  selectedMemberUUIDs: string[];

  // Loading and action states
  previewLoading: boolean;
  isLoading: boolean;
  isAuthenticated: boolean;

  // Actions
  onPurchase: () => void;
}

export default function PurchaseStep3({
  plans,
  selectedPlan,
  orderData,
  showCampaign,
  campaignCode,
  campaignError,
  onCampaignToggle,
  onCampaignCodeChange,
  onCampaignErrorClear,
  selectedForMyself,
  selectedMemberUUIDs,
  previewLoading,
  isLoading,
  isAuthenticated,
  onPurchase
}: PurchaseStep3Props) {
  const t = useTranslations();

  return (
    <Card>
      <CardHeader className="pb-0">
        <CardTitle className="flex items-center gap-3 sm:gap-2">
          <div className="flex items-center justify-center w-10 h-10 sm:w-8 sm:h-8 bg-primary text-primary-foreground rounded-full text-base sm:text-sm font-bold flex-shrink-0">
            {t('common.step3')}
          </div>
          <div className="flex items-center gap-2">
            <CreditCardIcon className="w-6 h-6 sm:w-5 sm:h-5 text-primary flex-shrink-0" />
            <span className="text-lg sm:text-base font-bold sm:font-semibold leading-tight text-foreground">
              {t('purchase.purchase.confirmAndPay')}
            </span>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pt-0 px-3 sm:px-6">
        {/* Total Price Display */}
        <div className="bg-gradient-to-r from-primary/5 to-primary/10 rounded-xl p-4 sm:p-5 shadow-sm">
            {/* Header Row: Title + Monthly Price and Total + Original Price */}
            <div className="flex flex-col lg:flex-row lg:justify-between lg:items-start space-y-4 lg:space-y-0 mb-4">
              {/* Left: Monthly Price and Plan Details */}
              <div className="flex-1">
                <div className="flex flex-col sm:flex-row sm:items-baseline sm:gap-3 mb-2">
                  <div className="text-base sm:text-sm text-primary font-medium">
                    {(() => {
                      const plan = plans.find(p => p.pid === selectedPlan);
                      const price = plan?.price ?? 0;
                      const months = plan?.month ?? 1;
                      const totalPrice = (price / 100).toFixed(2);
                      return `$${totalPrice} / ${months}${t('purchase.purchase.month')}`;
                    })()}
                  </div>
                </div>
                <p className="text-base sm:text-sm text-muted-foreground flex items-center font-medium leading-relaxed">
                  {(() => {
                    const planMonths = plans.find(p => p.pid === selectedPlan)?.month || 0;
                    const targetCount = (selectedForMyself ? 1 : 0) + selectedMemberUUIDs.length;
                    if (targetCount === 1) {
                      return t('purchase.purchase.proAuthorization', { months: planMonths });
                    } else {
                      return t('purchase.purchase.proAuthorizationMultiple', { months: planMonths, count: targetCount });
                    }
                  })()}
                </p>
              </div>

              {/* Right: Total Price with Original Price */}
              <div className="text-left lg:text-right">
                {orderData?.originAmount && orderData.originAmount > orderData.payAmount && (
                  <div className="text-base sm:text-sm text-muted-foreground line-through mb-1">
                    {"$"}{(orderData.originAmount / 100).toFixed(2)}
                  </div>
                )}
                <div className={`text-3xl sm:text-2xl font-black text-destructive flex items-center justify-start lg:justify-end ${previewLoading ? 'opacity-60' : ''}`}>
                  {previewLoading && (
                    <LoaderIcon className="w-5 h-5 sm:w-4 sm:h-4 animate-spin mr-2" />
                  )}
                  {"$"}{((orderData?.payAmount ?? plans.find(p => p.pid === selectedPlan)?.price ?? 0) / 100).toFixed(2)}
                </div>
                <div className="text-base sm:text-sm text-muted-foreground font-medium mt-1">
                  {t('purchase.purchase.includingTax')}
                </div>
              </div>
            </div>

            {/* Campaign Description */}
            {orderData?.campaign && (
              <div className="mb-4">
                <p className="text-base sm:text-sm text-secondary-foreground bg-secondary px-3 py-2 rounded-lg font-bold sm:font-semibold leading-relaxed">
                  {orderData.campaign.description}
                </p>
              </div>
            )}

            {/* Promo Code Section */}
            <div>
              {!showCampaign && !campaignCode && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onCampaignToggle(true)}
                  className="text-primary hover:text-primary/80 font-medium text-base sm:text-sm"
                >
                  <PlusIcon className="w-5 h-5 sm:w-4 sm:h-4 mr-2 sm:mr-1" />
                  {t('purchase.purchase.havePromoCode')}
                </Button>
              )}

              <Collapsible open={showCampaign} onOpenChange={onCampaignToggle}>
                <CollapsibleContent>
                  <div className="mt-3 sm:mt-2">
                    <div className="flex items-center space-x-2 sm:space-x-2">
                      <div className="relative flex-1">
                        <Input
                          placeholder={t('purchase.purchase.campaignCodePlaceholder')}
                          value={campaignCode}
                          onChange={(e) => {
                            onCampaignCodeChange(e.target.value);
                            onCampaignErrorClear();
                          }}
                          className={`text-lg sm:text-base py-3 sm:py-2 px-4 sm:px-3 ${campaignError ? 'border-destructive focus:ring-destructive' : 'border-border focus:border-primary'} ${previewLoading && campaignCode ? 'pr-10 sm:pr-8' : ''}`}
                        />
                        {previewLoading && campaignCode && (
                          <LoaderIcon className="absolute right-3 sm:right-2 top-1/2 transform -translate-y-1/2 w-5 h-5 sm:w-4 sm:h-4 animate-spin text-muted-foreground" />
                        )}
                      </div>
                      {campaignCode && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            onCampaignCodeChange('');
                            onCampaignErrorClear();
                            onCampaignToggle(false);
                          }}
                          className="text-base sm:text-sm font-medium px-4 sm:px-3 py-3 sm:py-2"
                        >
                          {t('purchase.purchase.clear')}
                        </Button>
                      )}
                    </div>
                    {campaignError && (
                      <div className="mt-2 sm:mt-1 flex items-center text-destructive text-base sm:text-sm font-medium">
                        <AlertTriangleIcon className="w-5 h-5 sm:w-4 sm:h-4 mr-2 sm:mr-1" />
                        {campaignError}
                      </div>
                    )}
                  </div>
                </CollapsibleContent>
              </Collapsible>
            </div>
          </div>

        {/* Purchase Button */}
        <div className="space-y-4 sm:space-y-4">
          <Button
            variant="destructive"
            size="lg"
            className="w-full font-black text-xl sm:text-lg py-8 sm:py-6 shadow-xl hover:shadow-2xl transition-all duration-300"
            onClick={onPurchase}
            disabled={
              !isAuthenticated ||
              plans.length === 0 ||
              !selectedPlan ||
              isLoading ||
              (!selectedForMyself && selectedMemberUUIDs.length === 0)
            }
          >
            {!isAuthenticated ? (
              <>
                <span className="text-2xl mr-2">{"📧"}</span>
                <span className="text-xl sm:text-lg">{t('purchase.purchase.bindEmailToPayNow')}</span>
              </>
            ) : isLoading ? (
              <>
                <LoaderIcon className="w-6 h-6 sm:w-5 sm:h-5 animate-spin mr-3 sm:mr-2" />
                <span className="text-xl sm:text-lg">{t('purchase.purchase.processing')}{t('purchase.purchase.ellipsis')}</span>
              </>
            ) : plans.length === 0 ? (
              <span className="text-xl sm:text-lg">{t('purchase.purchase.noPlans')}</span>
            ) : (!selectedForMyself && selectedMemberUUIDs.length === 0) ? (
              <span className="text-xl sm:text-lg">{t('purchase.purchase.selectTarget')}</span>
            ) : (
              <>
                <span className="text-2xl mr-2">{"🚀"}</span>
                <span className="text-xl sm:text-lg">{t('purchase.purchase.payNow')}</span>
              </>
            )}
          </Button>

        {/* Trust Signals */}
        <div className="flex flex-col sm:flex-row justify-center items-center gap-4 sm:gap-6 text-sm sm:text-xs text-muted-foreground py-6 sm:py-4">
          <div className="flex items-center gap-2 sm:gap-1 font-medium">
            <ShieldIcon className="w-5 h-5 sm:w-4 sm:h-4" />
            {t('purchase.purchase.securePayment')}
          </div>
          <div className="flex items-center gap-2 sm:gap-1 font-medium">
            <ZapIcon className="w-5 h-5 sm:w-4 sm:h-4" />
            {t('purchase.purchase.instantActivation')}
          </div>
          <div className="flex items-center gap-2 sm:gap-1 font-medium">
            <HeadphonesIcon className="w-5 h-5 sm:w-4 sm:h-4" />
            {t('purchase.purchase.support24x7')}
          </div>
        </div>
        </div>
      </CardContent>
    </Card>
  );
}