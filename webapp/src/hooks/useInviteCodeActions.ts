import { useTranslation } from 'react-i18next';
import { useAlert } from "../stores";
import { useUser } from "./useUser";

import { useShareLink } from './useShareLink';
import type { MyInviteCode } from '../services/api-types';
import { cloudApi } from '../services/cloud-api';

/**
 * 邀请码操作的自定义 Hook
 * 封装邀请码相关的操作逻辑，供 MyInviteCode 和 MyInviteCodeList 页面复用
 */
export function useInviteCodeActions() {
  const { t } = useTranslation();
  const { showAlert } = useAlert();
  const { user } = useUser();
  const { getShareLink, loading: shareLinkLoading } = useShareLink();


  /**
   * 分享邀请内容（模糊文案，不含产品名和 VPN 词汇）
   * 移动端使用系统分享，桌面端复制到剪贴板
   */
  const shareInviteCode = async (inviteCode: MyInviteCode) => {
    const shareLink = await getShareLink(inviteCode.code);
    if (!shareLink) {
      showAlert(t('invite:invite.getShareLinkFailed', '获取分享链接失败'), "error");
      return;
    }

    const copyContent = t('invite:invite.shareGiftText', { link: shareLink });

    if (window._platform?.share) {
      try {
        await window._platform.share({ text: copyContent });
        // Native share sheet is its own feedback — no toast needed
        return;
      } catch (error) {
        console.warn('Native share failed, falling back to clipboard:', error);
      }
    }

    try {
      await window._platform!.writeClipboard?.(copyContent);
      showAlert(t('invite:invite.shareContentCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailedPermission'), "error");
    }
  };

  /**
   * 复制分享链接
   * @param code 邀请码
   */
  const copyShareLink = async (code: string) => {
    if (!code) {
      showAlert(t('invite:invite.noShareLink'), "warning");
      return;
    }

    // 获取分享链接（带缓存）
    const shareLink = await getShareLink(code);
    if (!shareLink) {
      showAlert(t('invite:invite.getShareLinkFailed', '获取分享链接失败'), "error");
      return;
    }

    try {
      await window._platform!.writeClipboard?.(shareLink);
      showAlert(t('invite:invite.sharePageUrlCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailed'), "error");
    }
  };

  /**
   * 复制邀请码
   */
  const copyInviteCode = async (code: string) => {
    try {
      await window._platform!.writeClipboard?.(code.toUpperCase());
      showAlert(t('invite:invite.inviteCodeCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailed'), "error");
    }
  };

  /**
   * 更新邀请码备注
   * @returns 是否更新成功
   */
  const updateRemark = async (code: string, remark: string): Promise<boolean> => {
    try {
      const response = await cloudApi.request('PUT', `/api/invite/my-codes/${code}/remark`, { remark });
      if (response.code === 0) {
        showAlert(t('invite:invite.remarkUpdated'), "success");
        return true;
      } else {
        console.error('[useInviteCodeActions] Update remark failed:', response.code, response.message);
        showAlert(t('invite:invite.updateRemarkFailed'), 'error');
        return false;
      }
    } catch (error) {
      console.error(t('invite:invite.updateRemarkFailed'), error);
      showAlert(t('invite:invite.updateRemarkFailedRetry'), "error");
      return false;
    }
  };

  /**
   * 分享完整邀请内容（带有效期）
   * @param inviteCode 邀请码对象
   * @param expiresInDays 链接有效期（天数）
   */
  const shareInviteCodeWithExpiration = async (inviteCode: MyInviteCode, expiresInDays: number) => {
    // 获取分享链接（带缓存）
    const shareLink = await getShareLink(inviteCode.code, expiresInDays);
    if (!shareLink) {
      showAlert(t('invite:invite.getShareLinkFailed', '获取分享链接失败'), "error");
      return;
    }

    // 根据是否为分销商显示不同的奖励规则
    const rewardDays = inviteCode.config.purchaseRewardDays;
    const rewardText = user?.isRetailer
      ? `💳 ${t('invite:invite.inviteeReward')} ${rewardDays} ${t('invite:invite.days')}`
      : `💳 ${t('invite:invite.paidPurchase')} ${rewardDays} ${t('invite:invite.days')}`;

    const copyContent = `${t('invite:invite.inviteYouToUse')}

🎁 ${t('invite:invite.rewardRules')}:
${rewardText}

📱 ${t('invite:invite.downloadApp')}: ${shareLink}
🏷️ ${t('invite:invite.inviteCodeLabel')}: ${inviteCode.code.toUpperCase()}`;

    if (window._platform?.share) {
      try {
        await window._platform.share({
          title: t('invite:invite.inviteYouToUse'),
          text: copyContent,
        });
        return;
      } catch (error) {
        console.warn('Native share failed, falling back to clipboard:', error);
      }
    }

    // 桌面或分享失败时使用剪贴板
    try {
      await window._platform!.writeClipboard?.(copyContent);
      showAlert(t('invite:invite.shareContentCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailedPermission'), "error");
    }
  };

  /**
   * 复制分享链接（带有效期）
   * @param code 邀请码
   * @param expiresInDays 链接有效期（天数）
   */
  const copyShareLinkWithExpiration = async (code: string, expiresInDays: number) => {
    if (!code) {
      showAlert(t('invite:invite.noShareLink'), "warning");
      return;
    }

    // 获取分享链接（带缓存）
    const shareLink = await getShareLink(code, expiresInDays);
    if (!shareLink) {
      showAlert(t('invite:invite.getShareLinkFailed', '获取分享链接失败'), "error");
      return;
    }

    try {
      await window._platform!.writeClipboard?.(shareLink);
      showAlert(t('invite:invite.sharePageUrlCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailed'), "error");
    }
  };

  return {
    shareInviteCode,
    copyShareLink,
    copyInviteCode,
    updateRemark,
    shareLinkLoading,
    shareInviteCodeWithExpiration,
    copyShareLinkWithExpiration,
  };
}
