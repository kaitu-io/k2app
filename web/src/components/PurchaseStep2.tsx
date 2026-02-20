"use client";

import { useTranslations } from "next-intl";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { LoaderIcon, PackageIcon } from "lucide-react";
import type { Plan } from "@/lib/api";

export interface PurchaseStep2Props {
  plans: Plan[];
  selectedPlan: string;
  onPlanChange: (planId: string) => void;
  isLoading?: boolean;
}

export default function PurchaseStep2({
  plans,
  selectedPlan,
  onPlanChange,
  isLoading = false
}: PurchaseStep2Props) {
  const t = useTranslations();

  return (
    <Card>
      <CardHeader className="pb-0">
        <CardTitle className="flex items-center gap-3 sm:gap-2">
          <div className="flex items-center justify-center w-10 h-10 sm:w-8 sm:h-8 bg-primary text-primary-foreground rounded-full text-base sm:text-sm font-bold flex-shrink-0">
            {t('common.step2')}
          </div>
          <div className="flex items-center gap-2">
            <PackageIcon className="w-6 h-6 sm:w-5 sm:h-5 text-primary flex-shrink-0" />
            <span className="text-lg sm:text-base font-bold sm:font-semibold leading-tight text-foreground">
              {t('purchase.purchase.selectPlan')}
            </span>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-3 sm:px-6 pt-0">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <LoaderIcon className="w-6 h-6 animate-spin mr-2" />
            {t('purchase.purchase.loading')}
          </div>
        ) : plans.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground">
            {t('purchase.purchase.noPlansAvailable')}
          </div>
        ) : (
          <RadioGroup
            value={selectedPlan}
            onValueChange={onPlanChange}
            className="space-y-4 sm:space-y-4"
          >
            {plans
              .slice()
              .sort((a, b) => a.month - b.month)
              .map((item) => {
                const discount = item.originPrice > item.price ? (item.originPrice - item.price) / 100 : 0;
                const monthlyPrice = (item.price / item.month / 100).toFixed(2);
                const isRecommended = item.highlight;

                return (
                  <div
                    key={item.pid}
                    className={`relative border rounded-lg p-3 sm:p-6 transition-all cursor-pointer hover:shadow-lg overflow-hidden ${
                      selectedPlan === item.pid
                        ? 'border-primary bg-primary/5 shadow-md'
                        : isRecommended
                        ? 'border-destructive/50 bg-destructive/10'
                        : 'border-border hover:border-primary/50'
                    }`}
                    onClick={() => onPlanChange(item.pid)}
                  >
                    <div className="flex items-start space-x-3 sm:space-x-3">
                      <RadioGroupItem value={item.pid} id={item.pid} className="mt-1 sm:mt-0.5 flex-shrink-0 w-5 h-5 sm:w-4 sm:h-4" />
                      <div className="flex-1 min-w-0 overflow-hidden">
                        <Label htmlFor={item.pid} className="text-xl sm:text-xl font-black sm:font-bold cursor-pointer block leading-tight">
                          {t(`common.plan.pid.${item.pid}`)}
                        </Label>

                        {/* Responsive Layout */}
                        <div className="mt-2">
                          {/* Single flex container that wraps on narrow screens, horizontal on wider screens */}
                          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-2 md:gap-4">
                            {/* Left side: Monthly price and savings badge */}
                            <div className="flex items-baseline space-x-2 flex-wrap">
                              <span className="text-2xl font-black md:font-bold text-primary">
                                {"$"}{monthlyPrice}
                              </span>
                              <span className="text-base md:text-sm text-muted-foreground font-medium">
                                {t('purchase.purchase.priceSlash')}{t('purchase.purchase.month')}
                              </span>
                              {item.month > 1 && (
                                <span className="text-sm md:text-xs text-secondary-foreground font-bold md:font-medium bg-secondary px-2 py-1 rounded">
                                  {t('purchase.purchase.savePercent', { percent: Math.round((1 - item.price / item.originPrice) * 100) })}
                                </span>
                              )}
                            </div>

                            {/* Right side: Total price and original price - Always right aligned */}
                            <div className="flex flex-col text-right">
                              <div className="flex items-center justify-end space-x-2">
                                <span className="text-base md:text-sm text-muted-foreground font-medium md:hidden">
                                  {t('purchase.purchase.totalPrice')}
                                </span>
                                <span className="text-2xl font-black md:font-bold text-destructive">
                                  {"$"}{(item.price / 100).toFixed(2)}
                                </span>
                              </div>
                              <div className="hidden md:block text-sm text-muted-foreground">
                                {t('purchase.purchase.totalPrice')}
                              </div>
                              {discount > 0 && (
                                <div className="text-base md:text-sm text-muted-foreground line-through mt-1 md:mt-0">
                                  {"$"}{(item.originPrice / 100).toFixed(2)}
                                </div>
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>

                    {isRecommended && (
                      <div className="mt-3 sm:mt-4 text-center text-base sm:text-sm text-destructive font-bold sm:font-medium">
                        {"⭐"} {t('purchase.purchase.bestValue')} {t('purchase.purchase.dash')} {t('purchase.purchase.recommendedForMostUsers')}
                      </div>
                    )}
                  </div>
                );
              })}
          </RadioGroup>
        )}
      </CardContent>
    </Card>
  );
}