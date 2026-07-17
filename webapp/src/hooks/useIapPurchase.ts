/**
 * useIapPurchase — iOS StoreKit 2 IAP 购买流程 hook
 *
 * 信任模型（见 IIap 文档）：native 永不发放权益。
 * purchase/restore 返回 transactionId → webapp 调 Center
 * `/api/user/apple-iap/verify`（cloudApi 带鉴权）复核入账 → 入账成功后再
 * `finishTransaction`。verify 失败时**绝不** finishTransaction —— StoreKit 会通过
 * `onTransactionUpdate` 重投补单（这是关键安全网，不可绕过）。
 *
 * 直接读 `window._platform.iap`（无 props）。非 iOS 平台 iap 为 undefined。
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import type { IapProduct } from '../types/kaitu-core';
import type { DataUser } from '../services/api-types';
import { brandConfig } from '../brands';

/**
 * App Store 商品 id —— 品牌派生（brands/<id> 配置，与该品牌 iOS app 的 ASC 一致）。
 * 单一自动续订订阅：Apple 自动续订上限 1 年，只有 basic 一档。数组形态保留
 * （bridge `getProducts` 接收数组，未来加商品直接在品牌配置 append）。
 */
export const IAP_PRODUCT_IDS: readonly string[] = brandConfig.iapProductIds;

/** 用户缓存 key（与 useUser 保持一致） */
const USER_CACHE_KEY = 'api:user_info';

export interface UseIapPurchaseReturn {
  products: IapProduct[];
  loadProducts: () => Promise<void>;
  productsLoading: boolean;
  purchase: (productId: string, accountToken: string) => Promise<void>;
  restore: () => Promise<void>;
  purchasing: boolean;
  restoring: boolean;
  purchaseError: string | null;
  lastGrantedUser: DataUser | null;
  clearError: () => void;
}

export function useIapPurchase(): UseIapPurchaseReturn {
  const { t } = useTranslation();

  const [products, setProducts] = useState<IapProduct[]>([]);
  const [productsLoading, setProductsLoading] = useState(false);
  const [purchasing, setPurchasing] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [purchaseError, setPurchaseError] = useState<string | null>(null);
  const [lastGrantedUser, setLastGrantedUser] = useState<DataUser | null>(null);

  // Guard against setState after unmount (purchase/verify are long async chains).
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const clearError = useCallback(() => {
    setPurchaseError(null);
  }, []);

  const loadProducts = useCallback(async () => {
    const iap = window._platform?.iap;
    if (!iap) {
      console.warn('[useIapPurchase] iap not available on this platform');
      return;
    }
    setProductsLoading(true);
    try {
      const list = await iap.getProducts([...IAP_PRODUCT_IDS]);
      if (mountedRef.current) setProducts(list);
    } catch (err) {
      // Graceful degradation: leave products empty, sheet falls back to placeholders.
      console.error('[useIapPurchase] getProducts failed:', err);
    } finally {
      if (mountedRef.current) setProductsLoading(false);
    }
  }, []);

  /**
   * Verify a transaction with Center and grant entitlement.
   *
   * CRITICAL: on verify failure we return WITHOUT finishTransaction so StoreKit
   * re-delivers the transaction via onTransactionUpdate (auto-retry). Only finish
   * after a confirmed grant.
   */
  const verifyAndGrant = useCallback(
    async (transactionId: string): Promise<void> => {
      const iap = window._platform?.iap;
      if (!iap) {
        console.warn('[useIapPurchase] verifyAndGrant: iap missing');
        return;
      }
      try {
        const res = await cloudApi.post<DataUser>('/api/user/apple-iap/verify', {
          transactionId,
        });

        if (res.code !== 0 || !res.data) {
          // Do NOT finishTransaction — StoreKit will re-deliver via onTransactionUpdate.
          console.error(
            '[useIapPurchase] verify failed (no finish):',
            res.code,
            res.message,
          );
          if (mountedRef.current) {
            setPurchaseError(t('purchase:purchase.iap.verifyFailed'));
          }
          return;
        }

        // Grant: write fresh user into cache + expose to caller.
        cacheStore.set(USER_CACHE_KEY, res.data, { ttl: 3600 });
        if (mountedRef.current) setLastGrantedUser(res.data);

        // Finish is non-fatal: a finish failure must never unset the grant or
        // surface a user error (StoreKit re-delivers harmlessly; verify is idempotent).
        try {
          await iap.finishTransaction(transactionId);
        } catch (finishErr) {
          console.error(
            '[useIapPurchase] finishTransaction failed (non-fatal):',
            finishErr,
          );
        }
      } finally {
        if (mountedRef.current) {
          setPurchasing(false);
          setRestoring(false);
        }
      }
    },
    [t],
  );

  const purchase = useCallback(
    async (productId: string, accountToken: string): Promise<void> => {
      const iap = window._platform?.iap;
      if (!iap) {
        console.error('[useIapPurchase] purchase: IAP not available (code 593)');
        setPurchaseError(t('purchase:purchase.iap.purchaseFailed'));
        return;
      }
      setPurchasing(true);
      setPurchaseError(null);
      try {
        const result = await iap.purchase(productId, accountToken);

        if (result.result === 'cancelled') {
          // Silent no-op — user dismissed the Apple sheet.
          if (mountedRef.current) setPurchasing(false);
          return;
        }
        if (result.result === 'pending') {
          // Ask-to-Buy / SCA — entitlement arrives later via onTransactionUpdate.
          if (mountedRef.current) {
            setPurchaseError(t('purchase:purchase.iap.pendingApproval'));
            setPurchasing(false);
          }
          return;
        }
        // success
        if (result.transactionId) {
          await verifyAndGrant(result.transactionId);
        } else {
          console.error('[useIapPurchase] success without transactionId');
          if (mountedRef.current) {
            setPurchaseError(t('purchase:purchase.iap.purchaseFailed'));
            setPurchasing(false);
          }
        }
      } catch (err) {
        console.error('[useIapPurchase] purchase threw:', err);
        if (mountedRef.current) {
          setPurchaseError(t('purchase:purchase.iap.purchaseFailed'));
          setPurchasing(false);
        }
      }
    },
    [t, verifyAndGrant],
  );

  const restore = useCallback(async (): Promise<void> => {
    const iap = window._platform?.iap;
    if (!iap) {
      console.error('[useIapPurchase] restore: IAP not available');
      setPurchaseError(t('purchase:purchase.iap.purchaseFailed'));
      return;
    }
    setRestoring(true);
    setPurchaseError(null);
    try {
      const txns = await iap.restore();
      if (!txns || txns.length === 0) {
        if (mountedRef.current) {
          setPurchaseError(t('purchase:purchase.iap.nothingToRestore'));
          setRestoring(false);
        }
        return;
      }
      // SEQUENTIAL on purpose — Center uses FOR UPDATE row locks; parallel
      // verify calls deadlock. Never Promise.all here.
      for (const txn of txns) {
        await verifyAndGrant(txn.transactionId);
      }
    } catch (err) {
      console.error('[useIapPurchase] restore threw:', err);
      if (mountedRef.current) {
        setPurchaseError(t('purchase:purchase.iap.restoreFailed'));
      }
    } finally {
      if (mountedRef.current) setRestoring(false);
    }
  }, [t, verifyAndGrant]);

  // Background transaction updates: auto-renewals, interrupted purchases,
  // Ask-to-Buy approvals. Route each through the same verify→grant path.
  useEffect(() => {
    const iap = window._platform?.iap;
    if (!iap) return;
    const unsubscribe = iap.onTransactionUpdate((data) => {
      console.info('[useIapPurchase] onTransactionUpdate:', data.transactionId);
      void verifyAndGrant(data.transactionId);
    });
    return unsubscribe;
  }, [verifyAndGrant]);

  return {
    products,
    loadProducts,
    productsLoading,
    purchase,
    restore,
    purchasing,
    restoring,
    purchaseError,
    lastGrantedUser,
    clearError,
  };
}
