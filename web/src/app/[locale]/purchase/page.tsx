"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "@/i18n/routing";
import { useTranslations } from "next-intl";
import { useAuth } from "@/contexts/AuthContext";
import { toast } from "sonner";
import { useEmbedMode } from "@/hooks/useEmbedMode";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { useAppConfig } from "@/contexts/AppConfigContext";
import PurchaseStep1 from "@/components/PurchaseStep1";
import PurchaseStep2 from "@/components/PurchaseStep2";
import PurchaseStep3 from "@/components/PurchaseStep3";
import Header from "@/components/Header";
import Footer from "@/components/Footer";
import type { Plan, Order, CreateOrderRequest } from "@/lib/api";
import {
  TrophyIcon,
  AlertTriangleIcon,
  Loader2Icon,
} from "lucide-react";



function PayResultDialog({ 
  open, 
  order, 
  onSuccess, 
  onFail 
}: {
  open: boolean;
  order: Order | null;
  onSuccess: () => void;
  onFail: () => void;
}) {
  const t = useTranslations();
  
  return (
    <Dialog open={open} onOpenChange={() => {}}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('purchase.purchase.paymentResultConfirm')}</DialogTitle>
          <DialogDescription>
            <div className="flex flex-col gap-3 py-2">
              <div>
                <span className="font-semibold">
                  {t('purchase.purchase.orderNumber')}{t('common.colon')}
                </span>
                <span className="font-mono">{order?.uuid}</span>
              </div>
              <div>
                <span className="font-semibold text-gray-700 dark:text-gray-300">
                  {t('purchase.purchase.paymentAmount')}{t('common.colon')}
                </span>
                <span className="text-xl font-bold text-emerald-600 ml-1">
                  {"$"}{((order?.payAmount ?? 0) / 100).toFixed(2)}
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                {t('purchase.purchase.paymentConfirmHint')}
              </p>
            </div>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter className="flex flex-row justify-between">
          <Button variant="default" onClick={onSuccess} className="bg-green-600 hover:bg-green-700 text-white">
            {t('purchase.purchase.paymentSuccess')}
          </Button>
          <Button variant="outline" onClick={onFail} className="border-red-300 text-red-700 hover:bg-red-50">
            {t('purchase.purchase.paymentFailed')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function Purchase() {
  const router = useRouter();
  const t = useTranslations();
  const { isAuthenticated, isAuthLoading } = useAuth();

  // Initialize embed mode to handle auth_token URL parameter
  const { showNavigation, showFooter } = useEmbedMode();

  // State for each step
  const [selectedForMyself, setSelectedForMyself] = useState(true);
  const [selectedMemberUUIDs, setSelectedMemberUUIDs] = useState<string[]>([]);
  const [selectedPlan, setSelectedPlan] = useState("");
  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(false);

  // Payment and campaign state
  const [showCampaign, setShowCampaign] = useState(false);
  const [campaignCode, setCampaignCode] = useState("");
  const [campaignError, setCampaignError] = useState<string>("");
  const [orderData, setOrderData] = useState<Order | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [payDialogOpen, setPayDialogOpen] = useState(false);

  // Debounce timer for preview requests
  const previewTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Get user profile to check status
  const [userProfile, setUserProfile] = useState<{
    expiredAt: number;
    isFirstOrderDone: boolean;
    inviteCode?: {
      code: string;
    };
  } | null>(null);
  const [profileLoading, setProfileLoading] = useState(true);

  // Get app config from context
  const { appConfig, isLoading: appConfigLoading } = useAppConfig();

  // Note: Login redirect is handled by API layer when requests fail with 401

  // Fetch user profile
  useEffect(() => {
    const fetchUserProfile = async () => {
      try {
        const profile = await api.getUserProfile({ autoRedirectToAuth: false });
        setUserProfile(profile);
      } catch (error) {
        console.error("Failed to fetch user profile:", error);
      } finally {
        setProfileLoading(false);
      }
    };
    fetchUserProfile();
  }, []);

  // Check if user is expired
  const isExpired = userProfile ? (userProfile.expiredAt > 0 && userProfile.expiredAt < Date.now() / 1000) : false;

  // Fetch plans
  useEffect(() => {
    const fetchPlans = async () => {
      setPlansLoading(true);
      try {
        console.info('[Purchase] Starting to fetch plans');
        const data = await api.getPlans({ autoRedirectToAuth: false });
        console.info('[Purchase] API data:', data);

        if (data && data.items && Array.isArray(data.items)) {
          const planItems = data.items;
          console.info(`[Purchase] Got ${planItems.length} plans`);

          if (planItems.length === 0) {
            console.warn('[Purchase] No available plans');
            setPlans([]);
            setSelectedPlan("");
          } else {
            setPlans(planItems);
            // Default select first highlight=true plan, otherwise first plan
            const highlightPlan = planItems.find((p: Plan) => p.highlight);
            setSelectedPlan(highlightPlan ? highlightPlan.pid : planItems[0]?.pid || "");
          }
        } else {
          console.warn('[Purchase] Plan data format unexpected:', data);
          setPlans([]);
          setSelectedPlan("");
        }
      } catch (error) {
        console.error('[Purchase] Failed to fetch plans:', error);
        toast.error(t('purchase.purchase.getPlansFailedRetry'));
        setPlans([]);
        setSelectedPlan("");
      } finally {
        setPlansLoading(false);
      }
    };

    fetchPlans();
  }, [t]);

  /**
   * 预览订单请求 (Preview Order)
   *
   * 功能说明：
   * - 向后端发送 preview: true 的订单请求，获取实际订单金额预览
   * - 后端会根据以下因素计算最终价格：
   *   1. 活动码 (campaignCode) - 折扣、优惠活动
   *   2. 用户资格 - 新用户优惠、续费优惠等
   *   3. 邀请奖励 - 被邀请用户的额外天数
   *   4. 套餐价格 - 基础价格
   *   5. 购买对象数量 - 为自己或成员购买
   * - 返回的 orderData 包含：originAmount(原价)、payAmount(实付)、campaign(活动信息) 等
   * - 用于在用户点击支付前实时展示准确的订单金额
   *
   * 与 handleOrder 分离的原因：
   * - 避免 useCallback 依赖循环导致的无限重渲染
   * - preview 请求使用 previewLoading 状态，不影响支付按钮的 isLoading 状态
   */
  const fetchPreview = useCallback(async () => {
    // Check if any payment target is selected
    if (!selectedForMyself && selectedMemberUUIDs.length === 0) {
      return;
    }

    if (!selectedPlan) {
      return;
    }

    setPreviewLoading(true);

    try {
      console.info('[Purchase] Creating preview request:', { selectedPlan, campaignCode, selectedForMyself, selectedMemberUUIDs });

      const request: CreateOrderRequest = {
        preview: true,
        plan: selectedPlan,
        campaignCode: campaignCode || undefined,
        forMyself: selectedForMyself,
        forUserUUIDs: selectedMemberUUIDs.length > 0 ? selectedMemberUUIDs : undefined,
      };

      const data = await api.createOrder(request, { autoRedirectToAuth: false });
      console.info('[Purchase] Preview data:', data);

      const { order } = data;
      setOrderData(order);
      setCampaignError(""); // Clear previous errors
    } catch (error: unknown) {
      console.error('[Purchase] Preview exception:', error);

      // Check if it's a campaign code error
      if (error instanceof Error && error.message && error.message.includes('400001')) {
        setCampaignError(t('purchase.purchase.invalidCampaignCode'));
      } else {
        setCampaignError("");
      }
    } finally {
      setPreviewLoading(false);
    }
  }, [selectedForMyself, selectedMemberUUIDs, t, selectedPlan, campaignCode]);

  /**
   * 实际下单请求 (Create Order for Payment)
   *
   * 功能说明：
   * - 向后端发送 preview: false 的订单请求，创建真实订单
   * - 成功后跳转到支付页面 (payUrl)
   */
  const handleOrder = useCallback(async () => {
    // Check if any payment target is selected
    if (!selectedForMyself && selectedMemberUUIDs.length === 0) {
      toast.error(t('purchase.purchase.selectAtLeastOneTarget'));
      return;
    }

    setIsLoading(true);

    try {
      console.info('[Purchase] Creating order request:', { selectedPlan, campaignCode, selectedForMyself, selectedMemberUUIDs });

      const request: CreateOrderRequest = {
        preview: false,
        plan: selectedPlan,
        campaignCode: campaignCode || undefined,
        forMyself: selectedForMyself,
        forUserUUIDs: selectedMemberUUIDs.length > 0 ? selectedMemberUUIDs : undefined,
      };

      const data = await api.createOrder(request, { autoRedirectToAuth: false });
      console.info('[Purchase] Create order data:', data);

      const { order, payUrl } = data;
      setOrderData(order);
      setCampaignError(""); // Clear previous errors

      setPayDialogOpen(true);
      // Redirect to payment URL in current window
      window.location.href = payUrl;
    } catch (error: unknown) {
      console.error('[Purchase] Create order exception:', error);

      // Check if it's a campaign code error
      if (error instanceof Error && error.message && error.message.includes('400001')) {
        setCampaignError(t('purchase.purchase.invalidCampaignCode'));
      } else {
        setCampaignError("");
        setOrderData(null);
        toast.error(error instanceof Error ? error.message : t('purchase.purchase.createOrderFailed'));
      }
    } finally {
      setIsLoading(false);
    }
  }, [selectedForMyself, selectedMemberUUIDs, t, selectedPlan, campaignCode]);


  /**
   * 自动预览触发 (Auto Preview Trigger)
   *
   * 当以下条件变化时，自动触发预览请求：
   * - selectedPlan: 用户选择的套餐
   * - campaignCode: 用户输入的活动码
   * - selectedForMyself: 是否为自己购买
   * - selectedMemberUUIDs: 选择的成员列表
   *
   * 使用 300ms 防抖，避免频繁请求
   * 使用 selectedMemberUUIDsKey (字符串) 替代数组引用，避免不必要的重渲染
   */
  const selectedMemberUUIDsKey = selectedMemberUUIDs.join(',');

  useEffect(() => {
    // Skip if plans are still loading
    if (plansLoading) {
      return;
    }

    // Clear existing timeout
    if (previewTimeoutRef.current) {
      clearTimeout(previewTimeoutRef.current);
    }

    // Set new timeout for debounced request
    previewTimeoutRef.current = setTimeout(() => {
      if (selectedPlan && (selectedForMyself || selectedMemberUUIDs.length > 0)) {
        fetchPreview();
      }
    }, 300); // 300ms debounce delay

    // Cleanup on unmount or dependency change
    return () => {
      if (previewTimeoutRef.current) {
        clearTimeout(previewTimeoutRef.current);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [campaignCode, selectedPlan, plansLoading, selectedForMyself, selectedMemberUUIDsKey]);

  // Handle member selection change
  const handleMemberSelectionChange = useCallback((forMyself: boolean, memberUUIDs: string[]) => {
    setSelectedForMyself(forMyself);
    setSelectedMemberUUIDs(memberUUIDs);
  }, []);

  const handleLoginSuccess = useCallback(() => {
    // Login succeeded - no need to navigate steps since all are shown
  }, []);

  // Handle plan change
  const handlePlanChange = useCallback((planId: string) => {
    setSelectedPlan(planId);
  }, []);

  // Campaign code handlers
  const handleCampaignToggle = useCallback((show: boolean) => {
    setShowCampaign(show);
  }, []);

  const handleCampaignCodeChange = useCallback((code: string) => {
    setCampaignCode(code);
  }, []);

  const handleCampaignErrorClear = useCallback(() => {
    setCampaignError("");
  }, []);

  // Purchase handler
  const handlePurchase = useCallback(() => {
    handleOrder();
  }, [handleOrder]);

  const handlePaySuccess = useCallback(async () => {
    setPayDialogOpen(false);
    // Refresh user profile
    try {
      const profile = await api.getUserProfile({ autoRedirectToAuth: false });
      setUserProfile(profile);
    } catch (error) {
      console.error('Failed to refresh user profile', error);
    }
    // Navigate to install page after successful payment
    router.push('/install');
  }, [router]);

  const handlePayFail = useCallback(async () => {
    setPayDialogOpen(false);
    // Refresh user profile
    try {
      const profile = await api.getUserProfile({ autoRedirectToAuth: false });
      setUserProfile(profile);
    } catch (error) {
      console.error('Failed to refresh user profile', error);
    }
    // Navigate to install page even after payment failure
    router.push('/install');
  }, [router]);

  // Purchase page doesn't require authentication - skip redirect logic
  // Users can purchase without login, authentication is handled by API layer when needed

  if (isAuthLoading || profileLoading || appConfigLoading) {
    return (
      <>
        {showNavigation && <Header />}
        <div className="container max-w-4xl mx-auto py-4 px-4 sm:py-8 sm:px-6 lg:px-8">
          <div className="flex items-center justify-center">
            <Loader2Icon className="w-8 h-8 animate-spin" />
            <span className="ml-2">{t('purchase.purchase.loading')}</span>
          </div>
        </div>
        {showFooter && <Footer />}
      </>
    );
  }

  return (
    <>
      {/* Header - Hidden in embedded mode */}
      {showNavigation && <Header />}

      <div className="container max-w-4xl xl:max-w-6xl 2xl:max-w-7xl mx-auto py-4 px-0 sm:py-8 sm:px-6 lg:px-8 space-y-6 sm:space-y-8">
        {/* Status Alerts - Only show expired alert, remove trial mode alert */}
        {isExpired && (
          <div className="bg-orange-50 dark:bg-orange-950/20 border-l-4 border-orange-500 p-4 sm:p-6 rounded-r-lg">
            <div className="flex items-center">
              <AlertTriangleIcon className="w-10 h-10 sm:w-8 sm:h-8 text-orange-500 mr-3 sm:mr-4 flex-shrink-0" />
              <div>
                <h3 className="font-bold text-lg sm:text-xl text-orange-700 dark:text-orange-400 leading-tight">
                  {t('purchase.purchase.authorizationExpired')}
                </h3>
                <p className="text-base sm:text-sm text-orange-600 dark:text-orange-300 mt-2 sm:mt-1 leading-relaxed">
                  {t('purchase.purchase.selectPlanToRenew')}
                </p>
              </div>
            </div>
          </div>
        )}

        {/* Friend Referral Bonus - Only show for users with invite code who haven't completed first order */}
        {userProfile?.inviteCode && !userProfile?.isFirstOrderDone && appConfig?.inviteReward && (
          <div className="bg-gradient-to-r from-yellow-100 to-orange-100 border-l-4 border-orange-500 p-4 sm:p-6 rounded-r-lg">
            <div className="flex items-center">
              <TrophyIcon className="w-10 h-10 sm:w-8 sm:h-8 text-orange-500 mr-3 sm:mr-4 flex-shrink-0" />
              <span className="font-bold text-xl sm:text-lg text-gray-800 leading-tight">
                {t('purchase.purchase.friendReferralBonus', { days: appConfig.inviteReward.purchaseRewardDays })}
              </span>
            </div>
          </div>
        )}

        {/* Page Title */}
        <div className="text-center px-4 sm:px-0">
          <h1 className="text-3xl sm:text-4xl lg:text-5xl font-black text-slate-800 dark:text-slate-100 mb-3 sm:mb-4 leading-tight">
            {t('purchase.purchase.title')}
          </h1>
          <p className="text-lg sm:text-xl text-slate-600 dark:text-slate-300 font-medium leading-relaxed max-w-2xl mx-auto">
            {t('purchase.purchase.subtitle')}
          </p>
        </div>

        {/* All Steps - Mobile: Stacked, Desktop: Multi-column layout */}
        <div className="space-y-6 sm:space-y-8 xl:space-y-0 xl:grid xl:grid-cols-12 xl:gap-8">
          {/* Left Column - Steps 1 & 2 */}
          <div className="xl:col-span-7 2xl:col-span-8 space-y-6 sm:space-y-8">
            {/* Step 1: Email Binding and Target Selection */}
            <PurchaseStep1
              selectedForMyself={selectedForMyself}
              selectedMemberUUIDs={selectedMemberUUIDs}
              onMemberSelectionChange={handleMemberSelectionChange}
              onLoginSuccess={handleLoginSuccess}
            />

            {/* Step 2: Plan Selection */}
            <PurchaseStep2
              plans={plans}
              selectedPlan={selectedPlan}
              onPlanChange={handlePlanChange}
              isLoading={plansLoading}
            />
          </div>

          {/* Right Column - Step 3 */}
          <div className="xl:col-span-5 2xl:col-span-4">
            {/* Step 3: Confirmation and Payment - Sticky on desktop */}
            <div className="xl:sticky xl:top-8">
              <PurchaseStep3
              plans={plans}
              selectedPlan={selectedPlan}
              orderData={orderData}
              showCampaign={showCampaign}
              campaignCode={campaignCode}
              campaignError={campaignError}
              onCampaignToggle={handleCampaignToggle}
              onCampaignCodeChange={handleCampaignCodeChange}
              onCampaignErrorClear={handleCampaignErrorClear}
              selectedForMyself={selectedForMyself}
              selectedMemberUUIDs={selectedMemberUUIDs}
              previewLoading={previewLoading}
              isLoading={isLoading}
              isAuthenticated={isAuthenticated}
              onPurchase={handlePurchase}
              />
            </div>
          </div>
        </div>

        {/* Payment Result Dialog */}
        <PayResultDialog
          open={payDialogOpen}
          order={orderData}
          onSuccess={handlePaySuccess}
          onFail={handlePayFail}
        />
      </div>

      {/* Footer - Hidden in embedded mode */}
      {showFooter && <Footer />}
    </>
  );
}