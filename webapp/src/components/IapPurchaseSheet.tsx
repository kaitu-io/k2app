/**
 * IapPurchaseSheet — iOS StoreKit IAP 购买面板
 *
 * Apple 强制要件（缺任一项有再次拒审风险）：
 *  - 自动续订披露（价格 / 周期 / 自动续订条款，购买前可见）
 *  - 商品网格（Basic/Family × 月/年），显示本地化 displayName + displayPrice
 *  - 主购买按钮
 *  - Restore Purchases（恢复购买）
 *  - Manage Subscription（管理订阅，跳系统订阅页）
 *  - Terms of Service + Privacy Policy 链接
 *
 * 使用 MUI Dialog —— 绝不用 window.confirm/alert/prompt（iOS WebView 会静默吞掉，
 * 用户看到"点击没反应"）。镜像 Account.tsx 的 delete-account 对话框模式。
 */

import { useEffect, useMemo, useState } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Card,
  Stack,
  Typography,
  CircularProgress,
  Link,
  useTheme,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useIapPurchase, IAP_PRODUCT_IDS } from '../hooks/useIapPurchase';
import { useAppLinks } from '../hooks/useAppLinks';
import { useAlert } from '../stores/alert.store';
import { getThemeColors } from '../theme/colors';

interface IapPurchaseSheetProps {
  open: boolean;
  onClose: () => void;
  /** Center 派生的 RFC UUID（绑定开途账号），来自 user.appleAccountToken */
  accountToken: string;
}

/** Map a product id to its i18n label key when StoreKit metadata is unavailable. */
const PRODUCT_LABEL_KEY: Record<string, string> = {
  'io.kaitu.sub.basic.1m': 'purchase.iap.basicMonthly',
  'io.kaitu.sub.basic.1y': 'purchase.iap.basicYearly',
  'io.kaitu.sub.family.1m': 'purchase.iap.familyMonthly',
  'io.kaitu.sub.family.1y': 'purchase.iap.familyYearly',
};

const DEFAULT_SELECTED = 'io.kaitu.sub.basic.1y';

export default function IapPurchaseSheet({
  open,
  onClose,
  accountToken,
}: IapPurchaseSheetProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');
  const { links } = useAppLinks();
  const { showAlert } = useAlert();

  const {
    products,
    loadProducts,
    productsLoading,
    purchase,
    restore,
    purchasing,
    restoring,
    purchaseError,
    lastGrantedUser,
  } = useIapPurchase();

  const [selectedProductId, setSelectedProductId] = useState<string>(DEFAULT_SELECTED);

  // Load products whenever the sheet opens.
  useEffect(() => {
    if (open) {
      void loadProducts();
    }
  }, [open, loadProducts]);

  // On grant, surface success and close.
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      onClose();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastGrantedUser]);

  // Build the display rows: prefer StoreKit metadata, else placeholder fallback.
  const rows = useMemo(() => {
    return IAP_PRODUCT_IDS.map((id) => {
      const meta = products.find((p) => p.id === id);
      return {
        id,
        displayName: meta?.displayName || t(`purchase:${PRODUCT_LABEL_KEY[id]}`),
        displayPrice: meta?.displayPrice || '—',
      };
    });
  }, [products, t]);

  const openExternal = (url: string) => {
    void window._platform?.openExternal?.(url);
  };

  const handleManageSubscription = () => {
    void window._platform?.openExternal?.(
      'itms-apps://apps.apple.com/account/subscriptions',
    );
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="xs"
      fullWidth
      PaperProps={{ sx: { borderRadius: 3, background: theme.palette.background.paper } }}
    >
      <DialogTitle sx={{ fontWeight: 700 }}>
        {t('purchase:purchase.iap.title')}
      </DialogTitle>

      <DialogContent>
        <Stack spacing={2}>
          {/* Auto-renewal disclosure — Apple mandated, must be visible BEFORE purchase */}
          <Typography
            variant="body2"
            color="text.secondary"
            data-testid="iap-auto-renewal-disclosure"
            sx={{ fontSize: '0.8rem', lineHeight: 1.5 }}
          >
            {t('purchase:purchase.iap.autoRenewalDisclosure')}
          </Typography>

          {/* Product grid */}
          {productsLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
              <CircularProgress size={28} />
            </Box>
          ) : (
            <Stack spacing={1.5}>
              {rows.map((row) => {
                const isSelected = selectedProductId === row.id;
                return (
                  <Card
                    key={row.id}
                    variant="outlined"
                    onClick={() => setSelectedProductId(row.id)}
                    data-testid={`iap-product-${row.id}`}
                    sx={{
                      p: 2,
                      borderRadius: 2,
                      cursor: 'pointer',
                      borderWidth: isSelected ? 2 : 1,
                      borderColor: isSelected ? 'primary.main' : 'divider',
                      background: isSelected ? colors.selectedGradient : 'transparent',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                    }}
                  >
                    <Typography
                      variant="subtitle2"
                      fontWeight={isSelected ? 700 : 500}
                      color={isSelected ? 'primary.main' : 'text.primary'}
                    >
                      {row.displayName}
                    </Typography>
                    <Typography variant="subtitle2" fontWeight={700}>
                      {row.displayPrice}
                    </Typography>
                  </Card>
                );
              })}
            </Stack>
          )}

          {/* Error display */}
          {purchaseError && (
            <Typography
              variant="body2"
              color="error"
              data-testid="iap-error"
              sx={{ fontWeight: 600 }}
            >
              {purchaseError}
            </Typography>
          )}

          {/* Restore + Manage row */}
          <Stack direction="row" spacing={1} justifyContent="center">
            <Button
              size="small"
              onClick={() => void restore()}
              disabled={restoring || purchasing}
              sx={{ textTransform: 'none' }}
            >
              {restoring
                ? t('purchase:purchase.iap.restoring')
                : t('purchase:purchase.iap.restorePurchases')}
            </Button>
            <Button
              size="small"
              onClick={handleManageSubscription}
              sx={{ textTransform: 'none' }}
            >
              {t('purchase:purchase.iap.manageSubscription')}
            </Button>
          </Stack>

          {/* Legal links — Apple mandated */}
          <Stack direction="row" spacing={2} justifyContent="center">
            <Link
              component="button"
              type="button"
              variant="caption"
              underline="hover"
              onClick={() => openExternal(links.termsOfServiceUrl)}
            >
              {t('purchase:purchase.iap.terms')}
            </Link>
            <Link
              component="button"
              type="button"
              variant="caption"
              underline="hover"
              onClick={() => openExternal(links.privacyPolicyUrl)}
            >
              {t('purchase:purchase.iap.privacy')}
            </Link>
          </Stack>
        </Stack>
      </DialogContent>

      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} disabled={purchasing}>
          {t('common:common.cancel')}
        </Button>
        <Button
          variant="contained"
          color="error"
          onClick={() => void purchase(selectedProductId, accountToken)}
          disabled={purchasing || restoring}
          startIcon={purchasing ? <CircularProgress size={16} color="inherit" /> : undefined}
          sx={{ textTransform: 'none', fontWeight: 700 }}
        >
          {purchasing
            ? t('purchase:purchase.iap.subscribe')
            : t('purchase:purchase.iap.subscribeNow')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
