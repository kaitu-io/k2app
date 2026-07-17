/**
 * IosMembershipPanel — iOS 有效会员的「会员中心」（affordance = manage / status）。
 *
 * 组合（容器只编排，每个模块独立文件）：
 *   RenewalStatusCard  — 续订状态 + 到期倒计时（P0 核心，防流失）
 *   MembershipBenefits — 权益回顾（复用，数据取自 user 配额）
 *   InviteRewardCard   — 免费增长闭环（Apple 合规，仅 inviteReward 存在时）
 *   MembershipActions  — 付费记录 + 恢复购买（Restore 两态常驻，Apple 强制）
 *
 * 绝不出现购买 / 外链支付（满足 Apple 3.1.1）。subscribe 模式走 IosSubscribePanel。
 * 数据：affordance（mode + activeSub）由父级 Purchase 传入；user / appConfig 经 hook 自取，
 * 保持本面板对 iOS 会员态自洽。
 */

import { useEffect } from 'react';
import { Box, Stack, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import type { DataSubscription } from '../../services/api-types';
import { useIapPurchase } from '../../hooks/useIapPurchase';
import { useUser } from '../../hooks/useUser';
import { useAppConfig } from '../../hooks/useAppConfig';
import { useAlert } from '../../stores/alert.store';
import MembershipBenefits from '../MembershipBenefits';
import RenewalStatusCard from './RenewalStatusCard';
import InviteRewardCard from './InviteRewardCard';
import MembershipActions from './MembershipActions';

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
  const { restore, restoring, purchaseError, lastGrantedUser } = useIapPurchase();
  const { showAlert } = useAlert();

  // 恢复成功后：提示 + 刷新用户，使 affordance 重算（未绑定的 Apple 订阅被绑定后
  // status → manage 自动翻转）。
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      void fetchUser();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastGrantedUser]);

  const openManage = () => {
    const url =
      activeSub?.manage?.kind === 'url' && activeSub.manage.url ? activeSub.manage.url : APPLE_SUBS_URL;
    void window._platform?.openExternal?.(url);
  };

  const inviteRewardDays = appConfig?.inviteReward?.purchaseRewardDays ?? 0;

  return (
    <Box sx={{ width: '100%', py: 1 }} data-testid={`ios-membership-${mode}`}>
      <Stack spacing={2.5}>
        <Typography variant="h6" fontWeight={700}>
          {t('purchase:purchase.iap.membershipTitle')}
        </Typography>

        {/* 续订状态 + 到期倒计时 */}
        <RenewalStatusCard
          mode={mode}
          autoRenew={activeSub?.autoRenew}
          periodEndSec={activeSub?.currentPeriodEnd}
          expiredAtSec={user?.expiredAt ?? 0}
          onManage={openManage}
        />

        {/* 权益回顾 — 配额取自用户档（tier quota） */}
        <MembershipBenefits
          maxDevice={user?.maxDevice}
          maxRouterDevice={user?.maxRouterDevice}
          maxLanClient={user?.maxLanClient}
        />

        {/* 增长闭环 — 免费得天数（Apple 合规），仅配置存在时显示 */}
        {inviteRewardDays > 0 && (
          <InviteRewardCard
            days={inviteRewardDays}
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
    </Box>
  );
}
