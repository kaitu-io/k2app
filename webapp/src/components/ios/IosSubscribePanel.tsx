/**
 * IosSubscribePanel — iOS StoreKit 订阅入口（内联，非弹窗）
 *
 * iOS 只有单一自动续订订阅（brandConfig.iapProductIds[0]，品牌各绑各的商品 id），故绝不展示 Center 的多时长套餐
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
 * 未登录潜客（旧壳：没有设备账号）：先内联 EmailLoginForm，订阅按钮在登录前禁用
 * （StoreKit accountToken 来自已登录用户的 appleAccountToken）。登录后 useUser 刷新 →
 * 组件重渲染 → 订阅可用。新壳的零身份设备账号已登录，可直接订阅/试用，顶部给
 * 「已有账号？登录」入口（LoginDialog）。
 *
 * 试用（设计稿 ①，spec 2026-10-19 §4.2）：trialOfferOf() 判定当前 Apple ID 有免费
 * 试用资格时，显示徽标 / 时间线 / 「前 N 天免费」/ 试用 CTA 与试用披露；否则（含
 * 旧壳无 introOffer 字段）与原来一样是「立即订阅」。天数与价格全部来自 StoreKit。
 */

import { useEffect, useMemo, useState } from 'react';
import {
  Box,
  Card,
  Stack,
  Typography,
  Button,
  CircularProgress,
  Link,
} from '@mui/material';
import {
  Check as CheckIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useIapPurchase, IAP_PRODUCT_IDS } from '../../hooks/useIapPurchase';
import { useAppLinks } from '../../hooks/useAppLinks';
import { useUser } from '../../hooks/useUser';
import { useAlert } from '../../stores/alert.store';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { statsService } from '../../services/stats';
import EmailLoginForm from '../EmailLoginForm';
import { trialOfferOf } from './trial-offer';
import ConsumptionConsentSheet from './ConsumptionConsentSheet';

interface IosSubscribePanelProps {
  /** 是否已登录（决定先登录还是可直接订阅）。 */
  isAuthenticated: boolean;
  /** Center 派生的 RFC UUID（绑定开途账号），来自 user.appleAccountToken。 */
  accountToken: string;
  /** 是否已是会员（过期续订 banner 的前提）。 */
  isMembership: boolean;
  /** 会员是否已过期（续订 banner）。 */
  isExpired: boolean;
  /** 权益设备上限（来自匹配套餐，0/缺省走默认 5，与 MembershipBenefits 同口径）。 */
  maxDevice?: number;
}

/** 单一自动续订订阅商品 id（与 Center `1y` 对齐）。 */
const DEFAULT_SELECTED = IAP_PRODUCT_IDS[0];
const DEFAULT_DEVICE_COUNT = 5;

export default function IosSubscribePanel({
  isAuthenticated,
  accountToken,
  isMembership,
  isExpired,
  maxDevice,
}: IosSubscribePanelProps) {
  const { t } = useTranslation();
  const { links } = useAppLinks();
  const { showAlert } = useAlert();
  const { user, fetchUser } = useUser();
  const openLoginDialog = useLoginDialogStore((s) => s.open);

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

  // 退款同意弹层（设计稿 ④）：授予成功且从未问过时弹一次。
  const [consentOpen, setConsentOpen] = useState(false);

  // Load StoreKit products on mount.
  useEffect(() => {
    void loadProducts();
  }, [loadProducts]);

  // On grant: surface success + refresh user so the affordance flips subscribe → manage.
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      void fetchUser();
      if (lastGrantedUser.appleConsumptionConsent == null) setConsentOpen(true);
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
      trial: trialOfferOf(meta),
    };
  }, [products, t]);

  const trial = row.trial;
  const price = row.displayPrice;
  const deviceCount = maxDevice && maxDevice > 0 ? maxDevice : DEFAULT_DEVICE_COUNT;
  // 零身份设备账号：已登录但没有绑定邮箱 / Apple。旧壳未登录走下方 EmailLoginForm。
  const zeroIdentity = isAuthenticated && !!user && !(user.loginIdentifies?.length > 0);

  const openExternal = (url: string) => {
    void window._platform?.openExternal?.(url);
  };

  const handleManageSubscription = () => {
    void statsService.trackFunnel('manage_click');
    void window._platform?.openExternal?.(
      'itms-apps://apps.apple.com/account/subscriptions',
    );
  };

  const benefits = [
    t('purchase:purchase.iap.benefitStable'),
    t('purchase:purchase.iap.benefitAllNodes'),
    t('purchase:purchase.iap.benefitDevices', { count: deviceCount }),
  ];

  const timeline = trial
    ? [
        { title: t('purchase:purchase.iap.timelineToday'), body: t('purchase:purchase.iap.timelineTodayBody'), active: true },
        { title: t('purchase:purchase.iap.timelineCancelBy', { day: trial.days - 1 }), body: t('purchase:purchase.iap.timelineCancelByBody'), active: false },
        { title: t('purchase:purchase.iap.timelineCharge', { days: trial.days }), body: t('purchase:purchase.iap.timelineChargeBody', { price }), active: false },
      ]
    : [];

  return (
    <Box sx={{ width: '100%', py: 1 }} data-testid="ios-subscribe-panel">
      <Stack direction="column" spacing={2.5}>
        {/* 零身份设备账号：已有账号的用户从这里登录 */}
        {zeroIdentity && (
          <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: -1 }}>
            <Link
              component="button"
              type="button"
              variant="body2"
              underline="hover"
              data-testid="iap-have-account-login"
              onClick={() => openLoginDialog({ trigger: 'ios-subscribe-panel' })}
            >
              {t('purchase:purchase.iap.haveAccountLogin')}
            </Link>
          </Box>
        )}

        {/* 已过期会员：续订提示 */}
        {isMembership && isExpired && (
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

        {/* Hero：先讲稳定 */}
        <Stack spacing={1}>
          {trial && (
            <Box
              data-testid="iap-trial-badge"
              sx={{
                alignSelf: 'flex-start',
                fontSize: 13,
                fontWeight: 600,
                color: 'primary.contrastText',
                bgcolor: 'primary.main',
                borderRadius: 999,
                px: 1.25,
                py: 0.5,
              }}
            >
              {t('purchase:purchase.iap.trialBadge', { days: trial.days })}
            </Box>
          )}
          <Typography component="h1" sx={{ m: 0, fontSize: 28, lineHeight: 1.25, fontWeight: 700 }}>
            {t('purchase:purchase.iap.heroTitle')}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ fontSize: 15, lineHeight: 1.5 }}>
            {t('purchase:purchase.iap.heroSubtitle')}
          </Typography>
        </Stack>

        {/* 权益 */}
        <Stack spacing={1.5}>
          {benefits.map((text) => (
            <Box key={text} sx={{ display: 'flex', gap: 1.5, alignItems: 'flex-start' }}>
              <CheckIcon sx={{ color: 'primary.main', fontSize: 22, flex: 'none' }} />
              <Typography sx={{ fontSize: 15, lineHeight: 1.45 }}>{text}</Typography>
            </Box>
          ))}
        </Stack>

        {/* 未登录（旧壳）：先绑定邮箱登录 */}
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

        {/* 试用时间线：讲清何时扣费、怎么取消 */}
        {trial && (
          <Box
            data-testid="iap-trial-timeline"
            sx={{ bgcolor: 'action.hover', borderRadius: 3.5, p: 2, display: 'flex', flexDirection: 'column', gap: 1.75 }}
          >
            {timeline.map((step) => (
              <Box key={step.title} sx={{ display: 'flex', gap: 1.5 }}>
                <Box
                  sx={{
                    width: 10,
                    height: 10,
                    borderRadius: '50%',
                    mt: '5px',
                    flex: 'none',
                    boxSizing: 'border-box',
                    ...(step.active
                      ? { bgcolor: 'primary.main' }
                      : { border: '2px solid', borderColor: 'text.secondary' }),
                  }}
                />
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.25 }}>
                  <Typography sx={{ fontSize: 15, fontWeight: 600 }}>{step.title}</Typography>
                  <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>
                    {step.body}
                  </Typography>
                </Box>
              </Box>
            ))}
          </Box>
        )}

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
              px: 2,
              py: 1.75,
              borderRadius: 3.5,
              borderWidth: 2,
              borderColor: 'primary.main',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.25 }}>
              <Typography sx={{ fontSize: 16, fontWeight: 700 }}>
                {row.displayName}
              </Typography>
              {trial && (
                <Typography sx={{ fontSize: 13, color: 'primary.main' }}>
                  {t('purchase:purchase.iap.planTrialTag', { days: trial.days })}
                </Typography>
              )}
            </Box>
            <Typography sx={{ fontSize: 16, fontWeight: 700 }}>
              <span>{price}</span>
              <span>{t('purchase:purchase.iap.perYear')}</span>
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

        <Stack spacing={1.25}>
          {/* 主订阅按钮：品牌主色 */}
          <Button
            variant="contained"
            color="primary"
            size="large"
            fullWidth
            disableElevation
            data-testid="iap-subscribe-btn"
            onClick={() => {
              void statsService.trackFunnel('checkout_start', {
                plan: trial ? `${row.id}#trial` : row.id,
                channel: 'apple',
              });
              void purchase(row.id, accountToken);
            }}
            disabled={!isAuthenticated || purchasing || restoring}
            startIcon={purchasing ? <CircularProgress size={18} color="inherit" /> : undefined}
            sx={{
              fontWeight: 700,
              fontSize: 18,
              minHeight: 54,
              borderRadius: 3.5,
              textTransform: 'none',
              bgcolor: 'primary.main',
              color: 'primary.contrastText',
              '&:hover': { bgcolor: 'primary.dark' },
            }}
          >
            {purchasing
              ? t('purchase:purchase.iap.subscribe')
              : !isAuthenticated
                ? t('purchase:purchase.iap.loginToSubscribe')
                : trial
                  ? t('purchase:purchase.iap.startTrial', { days: trial.days })
                  : t('purchase:purchase.iap.subscribeNow')}
          </Button>

          {/* 自动续订披露 — Apple 强制，购买前可见 */}
          <Typography
            variant="body2"
            color="text.secondary"
            data-testid="iap-auto-renewal-disclosure"
            sx={{ fontSize: 12, lineHeight: 1.5, textAlign: 'center' }}
          >
            {trial
              ? t('purchase:purchase.iap.trialDisclosure', { days: trial.days, price })
              : t('purchase:purchase.iap.autoRenewalDisclosure', { price })}
          </Typography>
        </Stack>

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
      <ConsumptionConsentSheet open={consentOpen} onClose={() => setConsentOpen(false)} />
    </Box>
  );
}
