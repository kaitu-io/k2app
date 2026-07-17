/**
 * useStripeCheckout — overleap Stripe 订阅购买/管理动作。
 *
 * 两个动作都由 Center 生成一次性 URL（checkout session / billing portal），
 * 客户端只负责跳转外部浏览器完成；权益经 Stripe webhook 入账，客户端通过
 * 刷新用户信息感知（无本地入账逻辑）。
 */
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../services/cloud-api';
import { getErrorMessage } from '../utils/errorCode';

interface StripeRedirect {
  url: string;
}

export interface UseStripeCheckoutReturn {
  /** 创建 Checkout Session 并打开外链；true = 已打开。 */
  checkout: (planPid: string) => Promise<boolean>;
  /** 打开 Billing Portal（订阅管理/取消）；true = 已打开。 */
  openPortal: () => Promise<boolean>;
  loading: boolean;
  error: string | null;
  clearError: () => void;
}

export function useStripeCheckout(): UseStripeCheckoutReturn {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const openUrl = useCallback(
    async (path: string, body: Record<string, unknown>): Promise<boolean> => {
      setLoading(true);
      setError(null);
      try {
        const res = await cloudApi.post<StripeRedirect>(path, body);
        if (res.code !== 0 || !res.data?.url) {
          console.error('[useStripeCheckout] failed:', path, res.code, res.message);
          // Code-based mapping (never raw response.message) — see webapp/CLAUDE.md
          // "API Error Code Constitution".
          setError(getErrorMessage(res.code, res.message, t));
          return false;
        }
        void window._platform?.openExternal?.(res.data.url);
        return true;
      } catch (err) {
        console.error('[useStripeCheckout] threw:', path, err);
        setError(t('common:common.unknownError'));
        return false;
      } finally {
        setLoading(false);
      }
    },
    [t],
  );

  const checkout = useCallback(
    (planPid: string) => openUrl('/api/user/stripe/checkout', { plan: planPid }),
    [openUrl],
  );
  const openPortal = useCallback(() => openUrl('/api/user/stripe/portal', {}), [openUrl]);
  const clearError = useCallback(() => setError(null), []);

  return { checkout, openPortal, loading, error, clearError };
}
