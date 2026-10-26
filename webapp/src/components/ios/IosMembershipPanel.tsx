/**
 * IosMembershipPanel — iOS 有效会员的「会员中心」（affordance = manage / status）。
 *
 * 组合（容器只编排，每个模块独立文件）：
 *   RenewalStatusCard  — 续订状态 + 到期倒计时（P0 核心，防流失）
 *   TrialStatusCard    — 订阅处于 Apple 免费试用期（isTrial）时取代上一项（设计稿 ②）
 *   MembershipBenefits — 权益回顾（复用，数据取自 user 配额）
 *   InviteRewardCard   — 免费增长闭环（Apple 合规，仅 inviteReward 存在时）
 *   MembershipActions  — 付费记录 + 恢复购买（Restore 两态常驻，Apple 强制）
 *
 * 绝不出现购买 / 外链支付（满足 Apple 3.1.1）。subscribe 模式走 IosSubscribePanel。
 * 数据：affordance（mode + activeSub）由父级 Purchase 传入；user / appConfig 经 hook 自取，
 * 保持本面板对 iOS 会员态自洽。
 */

import { useEffect, useRef, useState } from 'react';
import { Box, Stack, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import type { DataSubscription } from '../../services/api-types';
import { useIapPurchase, IAP_PRODUCT_IDS } from '../../hooks/useIapPurchase';
import { useStripeCheckout } from '../../hooks/useStripeCheckout';
import { useUser } from '../../hooks/useUser';
import { useAppConfig } from '../../hooks/useAppConfig';
import { getCurrentAppConfig } from '../../config/apps';
import { useAlert } from '../../stores/alert.store';
import { statsService } from '../../services/stats';
import MembershipBenefits from '../MembershipBenefits';
import RenewalStatusCard from './RenewalStatusCard';
import TrialStatusCard from './TrialStatusCard';
import InviteRewardCard from './InviteRewardCard';
import MembershipActions from './MembershipActions';
import ConsumptionConsentSheet from './ConsumptionConsentSheet';

interface IosMembershipPanelProps {
  /** affordance 模式（仅 manage/status 渲染此面板；subscribe 走 IosSubscribePanel）。 */
  mode: 'manage' | 'status';
  /** 活跃订阅（manage 模式存在）：携 autoRenew / currentPeriodEnd / manage 面。 */
  activeSub?: DataSubscription;
}

const APPLE_SUBS_URL = 'itms-apps://apps.apple.com/account/subscriptions';

export default function IosMembershipPanel({ mode, activeSub }: IosMembershipPanelProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { user, fetchUser } = useUser();
  const { appConfig } = useAppConfig();
  const { products, loadProducts, restore, restoring, purchaseError, lastGrantedUser } = useIapPurchase();
  const isTrial = activeSub?.isTrial === true;

  // 试用卡的总天数 / 续订价格来自 StoreKit 商品——只在试用态才去取。
  useEffect(() => {
    if (isTrial) void loadProducts();
  }, [isTrial, loadProducts]);
  const trialProduct = products.find((p) => p.id === IAP_PRODUCT_IDS[0]) ?? products[0];
  const { openPortal } = useStripeCheckout();
  const { showAlert } = useAlert();

  // 退款同意弹层（设计稿 ④）：状态驱动——用户有 Apple 订阅且从未回答过（null/缺省）时，
  // 本面板每次挂载最多弹一次。购买成功后 Purchase 会把订阅面板换成本面板，所以不能挂在
  // 订阅面板的「授予成功」事件上。回答后 PUT 回写用户缓存，不会再开；失败 / 点遮罩 = 不写状态。
  const [consentOpen, setConsentOpen] = useState(false);
  const consentAsked = useRef(false);
  const needsConsent =
    !!user?.subscriptions?.some((s) => s.provider === 'apple') && user.appleConsumptionConsent == null;
  useEffect(() => {
    if (needsConsent && !consentAsked.current) {
      consentAsked.current = true;
      setConsentOpen(true);
    }
  }, [needsConsent]);

  // 恢复成功后：提示 + 刷新用户，使 affordance 重算（未绑定的 Apple 订阅被绑定后
  // status → manage 自动翻转）。
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      void fetchUser();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastGrantedUser]);

  // 按 manage.kind 分派（与 StripePurchasePanel 的 manage 面一致）：本品牌同时
  // 开了 apple_iap + stripe_checkout，同一个 iOS 会员中心可能对应任一 provider——
  // stripe_portal 必须走 Billing Portal，不能塌缩成 App Store 订阅页。
  const openManage = () => {
    void statsService.trackFunnel('manage_click');
    const manage = activeSub?.manage;
    if (manage?.kind === 'stripe_portal') {
      void openPortal();
      return;
    }
    if (manage?.kind === 'url' && manage.url) {
      void window._platform?.openExternal?.(manage.url);
      return;
    }
    void window._platform?.openExternal?.(APPLE_SUBS_URL);
  };

  const inviteRewardDays = appConfig?.inviteReward?.purchaseRewardDays ?? 0;

  return (
    <Box sx={{ width: '100%', py: 1 }} data-testid={`ios-membership-${mode}`}>
      <Stack spacing={2.5}>
        <Typography variant="h6" fontWeight={700}>
          {t('purchase:purchase.iap.membershipTitle')}
        </Typography>

        {/* 试用中：试用卡；否则续订状态 + 到期倒计时 */}
        {isTrial ? (
          <TrialStatusCard
            trialEndsAtSec={activeSub?.trialEndsAt ?? 0}
            product={trialProduct}
            onManage={openManage}
          />
        ) : (
          <RenewalStatusCard
            mode={mode}
            autoRenew={activeSub?.autoRenew}
            periodEndSec={activeSub?.currentPeriodEnd}
            expiredAtSec={user?.expiredAt ?? 0}
            onManage={openManage}
          />
        )}

        {/* 权益回顾 — 配额取自用户档（tier quota） */}
        <MembershipBenefits
          maxDevice={user?.maxDevice}
          maxRouterDevice={user?.maxRouterDevice}
          maxLanClient={user?.maxLanClient}
        />

        {/* 增长闭环 — 免费得天数（Apple 合规），仅邀请计划品牌（features.invite）且配置存在时显示 */}
        {getCurrentAppConfig().features.invite && inviteRewardDays > 0 && (
          <InviteRewardCard
            months={appConfig?.inviteReward?.minRewardMonths ?? 12}
            onInvite={() => navigate('/invite')}
          />
        )}

        {/* 自助操作 — 付费记录 + 恢复购买 */}
        <MembershipActions
          onHistory={() => navigate('/pro-histories?from=/purchase')}
          onRestore={() => void restore()}
          restoring={restoring}
        />

        {purchaseError && (
          <Typography
            variant="body2"
            color="error"
            data-testid="ios-membership-restore-error"
            sx={{ fontWeight: 600, textAlign: 'center' }}
          >
            {purchaseError}
          </Typography>
        )}
      </Stack>
      <ConsumptionConsentSheet open={consentOpen} onClose={() => setConsentOpen(false)} />
    </Box>
  );
}
