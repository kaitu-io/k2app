/**
 * IosSubscribePanel — iOS StoreKit 订阅入口（内联，非弹窗）
 *
 * iOS 只有单一自动续订订阅（io.kaitu.sub.basic.1y），故绝不展示 Center 的多时长套餐
 * 列表。本面板把会员权益 + 单一 StoreKit 商品 + 订阅/恢复/管理/法律链接全部内联在
 * Purchase 页内，取代旧的 IapPurchaseSheet 弹窗。
 *
 * Apple 强制要件（缺任一项有再次拒审风险）：
 *  - 自动续订披露（价格 / 周期 / 自动续订条款，购买前可见）
 *  - 商品行（StoreKit 本地化 displayName + displayPrice，绝不硬编码价格）
 *  - 主订阅按钮
 *  - Restore Purchases（恢复购买）
 *  - Manage Subscription（管理订阅，跳系统订阅页）
 *  - Terms of Service + Privacy Policy 链接
 *
 * 未登录潜客（affordance=subscribe 且无账号）：先内联 EmailLoginForm，订阅按钮在登录
 * 前禁用（StoreKit accountToken 来自已登录用户的 appleAccountToken）。登录后 useUser
 * 刷新 → 组件重渲染 → 订阅可用。
 */

import { useEffect, useMemo } from 'react';
import {
  Box,
  Card,
  Stack,
  Typography,
  Button,
  CircularProgress,
  Link,
  useTheme,
} from '@mui/material';
import {
  EmojiEvents as EmojiEventsIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useIapPurchase, IAP_PRODUCT_IDS } from '../../hooks/useIapPurchase';
import { useAppLinks } from '../../hooks/useAppLinks';
import { useUser } from '../../hooks/useUser';
import { useAlert } from '../../stores/alert.store';
import { getThemeColors } from '../../theme/colors';
import MembershipBenefits from '../MembershipBenefits';
import EmailLoginForm from '../EmailLoginForm';

interface IosSubscribePanelProps {
  /** 是否已登录（决定先登录还是可直接订阅）。 */
  isAuthenticated: boolean;
  /** Center 派生的 RFC UUID（绑定开途账号），来自 user.appleAccountToken。 */
  accountToken: string;
  /** 是否已是会员（首购 banner）。 */
  isMembership: boolean;
  /** 会员是否已过期（续订 banner）。 */
  isExpired: boolean;
  /** 权益设备上限（来自匹配套餐，缺省走默认值）。 */
  maxDevice?: number;
  maxRouterDevice?: number;
  maxLanClient?: number;
}

/** 单一自动续订订阅商品 id（与 Center `1y` 对齐）。 */
const DEFAULT_SELECTED = IAP_PRODUCT_IDS[0];

export default function IosSubscribePanel({
  isAuthenticated,
  accountToken,
  isMembership,
  isExpired,
  maxDevice,
  maxRouterDevice,
  maxLanClient,
}: IosSubscribePanelProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');
  const { links } = useAppLinks();
  const { showAlert } = useAlert();
  const { fetchUser } = useUser();

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

  // Load StoreKit products on mount.
  useEffect(() => {
    void loadProducts();
  }, [loadProducts]);

  // On grant: surface success + refresh user so the affordance flips subscribe → manage.
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      void fetchUser();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastGrantedUser]);

  // Single product row. Price stays StoreKit-authoritative (never hardcode), but
  // the NAME comes from our i18n — StoreKit's displayName carries the ASC brand
  // string ("Kaitu Annual"), which must not surface in-app (brand rule: 开途, never
  // Kaitu, in Chinese contexts). planName is brand-neutral across all locales.
  const row = useMemo(() => {
    const meta = products.find((p) => p.id === DEFAULT_SELECTED) ?? products[0];
    return {
      id: meta?.id ?? DEFAULT_SELECTED,
      displayName: t('purchase:purchase.iap.planName'),
      displayPrice: meta?.displayPrice || '—',
    };
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
    <Box sx={{ width: '100%', py: 1 }} data-testid="ios-subscribe-panel">
      <Stack direction="column" spacing={2.5}>
        {/* 状态提示：未付费首购 / 已过期续订 */}
        {!isMembership ? (
          <Box sx={{
            borderRadius: 2,
            border: '2px solid',
            borderColor: 'warning.main',
            bgcolor: 'warning.50',
            p: 1.5,
            display: 'flex',
            alignItems: 'center',
          }}>
            <EmojiEventsIcon sx={{ color: 'warning.main', fontSize: 28, mr: 1.5 }} />
            <Box>
              <Typography variant="subtitle1" fontWeight="bold" color="warning.dark" component="span">
                {t('purchase:purchase.completeFirstPurchase')}
              </Typography>
              <Typography variant="body2" color="warning.dark" sx={{ mt: 0.2, fontSize: '0.85rem' }} component="span">
                {t('purchase:purchase.currentlyInTrial')}
              </Typography>
            </Box>
          </Box>
        ) : isExpired && (
          <Box sx={{
            borderRadius: 2,
            border: '2px solid',
            borderColor: 'error.main',
            bgcolor: 'error.50',
            p: 1.5,
            display: 'flex',
            alignItems: 'center',
          }}>
            <WarningIcon sx={{ color: 'error.main', fontSize: 28, mr: 1.5 }} />
            <Box>
              <Typography variant="subtitle1" fontWeight="bold" color="error.main" component="span">
                {t('purchase:purchase.authorizationExpired')}
              </Typography>
              <Typography variant="body2" color="error.dark" sx={{ mt: 0.2, fontSize: '0.85rem' }} component="span">
                {t('purchase:purchase.selectPlanToRenew')}
              </Typography>
            </Box>
          </Box>
        )}

        {/* 会员权益 — 先展示价值 */}
        <MembershipBenefits
          maxDevice={maxDevice}
          maxRouterDevice={maxRouterDevice}
          maxLanClient={maxLanClient}
        />

        {/* 未登录：先绑定邮箱登录 */}
        {!isAuthenticated && (
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
              {t('purchase:purchase.bindEmail')}
            </Typography>
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <EmailLoginForm />
            </Card>
          </Box>
        )}

        {/* 自动续订披露 — Apple 强制，购买前可见 */}
        <Typography
          variant="body2"
          color="text.secondary"
          data-testid="iap-auto-renewal-disclosure"
          sx={{ fontSize: '0.8rem', lineHeight: 1.5 }}
        >
          {t('purchase:purchase.iap.autoRenewalDisclosure')}
        </Typography>

        {/* 单一 StoreKit 商品行 */}
        {productsLoading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
            <CircularProgress size={28} />
          </Box>
        ) : (
          <Card
            variant="outlined"
            data-testid={`iap-product-${row.id}`}
            sx={{
              p: 2,
              borderRadius: 2,
              borderWidth: 2,
              borderColor: 'primary.main',
              background: colors.selectedGradient,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <Typography variant="subtitle1" fontWeight={700} color="primary.main">
              {row.displayName}
            </Typography>
            <Typography variant="subtitle1" fontWeight={700}>
              {row.displayPrice}
            </Typography>
          </Card>
        )}

        {/* 错误展示 */}
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

        {/* 主订阅按钮 */}
        <Button
          variant="contained"
          color="error"
          size="large"
          fullWidth
          data-testid="iap-subscribe-btn"
          onClick={() => void purchase(row.id, accountToken)}
          disabled={!isAuthenticated || purchasing || restoring}
          startIcon={purchasing ? <CircularProgress size={18} color="inherit" /> : undefined}
          sx={{
            fontWeight: 700,
            fontSize: 18,
            py: 1.75,
            borderRadius: 3,
            textTransform: 'none',
            boxShadow: colors.errorGlowStrong,
            background: colors.errorGradient,
            transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
            '&:hover': {
              boxShadow: colors.errorGlowStrong,
              transform: 'translateY(-2px)',
              background: `linear-gradient(135deg, ${colors.errorDark} 0%, ${colors.error} 100%)`,
            },
            '&:active': { transform: 'translateY(0)' },
            '&:disabled': {
              background: colors.disabledGradient,
              boxShadow: 'none',
            },
          }}
        >
          {purchasing
            ? t('purchase:purchase.iap.subscribe')
            : !isAuthenticated
              ? t('purchase:purchase.iap.loginToSubscribe')
              : t('purchase:purchase.iap.subscribeNow')}
        </Button>

        {/* Restore + Manage */}
        <Stack direction="row" spacing={1} justifyContent="center">
          <Button
            size="small"
            onClick={() => void restore()}
            disabled={restoring || purchasing}
            data-testid="iap-restore-btn"
            sx={{ textTransform: 'none' }}
          >
            {restoring
              ? t('purchase:purchase.iap.restoring')
              : t('purchase:purchase.iap.restorePurchases')}
          </Button>
          <Button
            size="small"
            onClick={handleManageSubscription}
            data-testid="iap-manage-btn"
            sx={{ textTransform: 'none' }}
          >
            {t('purchase:purchase.iap.manageSubscription')}
          </Button>
        </Stack>

        {/* 法律链接 — Apple 强制 */}
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
    </Box>
  );
}
