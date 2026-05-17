import { useTranslation } from 'react-i18next';
import { useAlert } from "../stores";

import { useShareLink } from './useShareLink';
import { useAppConfig } from './useAppConfig';
import type { MyInviteCode } from '../services/api-types';
import { cloudApi } from '../services/cloud-api';

/**
 * 邀请码操作的自定义 Hook
 * 封装邀请码相关的操作逻辑，供 MyInviteCode 和 MyInviteCodeList 页面复用
 */
export function useInviteCodeActions() {
  const { t } = useTranslation();
  const { showAlert } = useAlert();
  const { getShareLink, loading: shareLinkLoading } = useShareLink();
  const { appConfig } = useAppConfig();


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
   * 复制推广链接（明文 /s/{code}，不带 token、不过期）
   * 与 InviteHub 的「复制推广链接」按钮行为一致
   */
  const copyPromotionLink = async (code: string) => {
    const baseURL = appConfig?.appLinks?.baseURL || 'https://kaitu.io';
    const link = `${baseURL}/s/${code}`;
    try {
      await window._platform!.writeClipboard?.(link);
      showAlert(t('invite:invite.promotionLinkCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailedPermission'), "error");
    }
  };

  return {
    shareInviteCode,
    copyInviteCode,
    updateRemark,
    shareLinkLoading,
    copyPromotionLink,
  };
}
