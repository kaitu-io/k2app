import { useState, useEffect, useCallback, memo, useRef, useMemo } from "react";
import {
  Box,
  Typography,
  Button,
  TextField,
  InputAdornment,
  Radio,
  Chip,
  Stack,
  Collapse,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Card,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  useTheme,
} from "@mui/material";
import { Add as AddIcon, EmojiEvents as EmojiEventsIcon, Error as ErrorIcon } from "@mui/icons-material";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAlert, useAuthStore } from "../stores";
import { useUser } from "../hooks/useUser";
import { useLoginDialogStore } from "../stores/login-dialog.store";

import { type Plan, type Order, type AppConfig } from "../services/api-types";
import { ERROR_CODES, getErrorMessage } from "../utils/errorCode";
import { LoadingState, EmptyPlans } from '../components/LoadingAndEmpty';
import MembershipBenefits from '../components/MembershipBenefits';
import EmailLoginForm from '../components/EmailLoginForm';
import { IosSubscribePanel, IosMembershipPanel } from '../components/ios';
import StripePurchasePanel from '../components/stripe/StripePurchasePanel';
import { useSubscriptionAffordance } from '../hooks/useSubscriptionAffordance';
import {
  Warning as WarningIcon,
  CheckCircle as CheckCircleIcon,
} from "@mui/icons-material";
import { getThemeColors } from '../theme/colors';
import { cloudApi } from '../services/cloud-api';
import { brandConfig } from '../brands';
import { cacheStore } from '../services/cache-store';
import { formatBytes } from '../utils/ui';

// 斜角彩带组件
function Ribbon({ text }: { text: string }) {
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  return (
    <Box
      sx={{
        position: 'absolute',
        top: -8,
        left: 8,
        px: 2,
        py: 0.5,
        background: colors.warningGradient,
        color: '#fff',
        fontWeight: 700,
        fontSize: 12,
        textAlign: 'center',
        borderRadius: 1,
        boxShadow: 2,
        zIndex: 3,
        letterSpacing: 1,
        userSelect: 'none',
        pointerEvents: 'none',
        transform: 'rotate(-8deg)',
        opacity: 0.95,
      }}
    >
      {text}
    </Box>
  );
}


// 支付结果对话框
function PayResultDialog({ open, order, onSuccess, onFail }: {
  open: boolean;
  order: Order | null;
  onSuccess: () => void;
  onFail: () => void;
}) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  return (
    <Dialog
      open={open}
      maxWidth="xs"
      fullWidth
      PaperProps={{
        sx: {
          borderRadius: 3,
          boxShadow: colors.selectedShadow,
          background: theme.palette.background.paper,
          overflow: 'visible',
        }
      }}
    >
      {/* Header with gradient background */}
      <Box
        sx={{
          background: colors.bgGradient,
          pt: 3,
          pb: 2,
          px: 3,
          position: 'relative',
          overflow: 'hidden',
          '&::before': {
            content: '""',
            position: 'absolute',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'radial-gradient(circle at top right, rgba(255, 255, 255, 0.15), transparent)',
            pointerEvents: 'none',
          }
        }}
      >
        <DialogTitle sx={{
          p: 0,
          color: 'white',
          fontWeight: 700,
          fontSize: '1.25rem',
          textAlign: 'center',
          position: 'relative',
          zIndex: 1,
        }}>
          {t('purchase:purchase.paymentResultConfirm')}
        </DialogTitle>
      </Box>

      {/* Content with card-like design */}
      <DialogContent sx={{ px: 3, pt: 3, pb: 2 }}>
        <Stack spacing={2.5}>
          {/* Order Info Card */}
          <Card
            variant="outlined"
            sx={{
              borderRadius: 2,
              borderColor: theme.palette.mode === 'dark' ? 'rgba(255, 255, 255, 0.12)' : 'grey.300',
              background: theme.palette.mode === 'dark'
                ? 'rgba(255, 255, 255, 0.02)'
                : 'rgba(0, 0, 0, 0.01)',
              p: 2,
            }}
          >
            <Stack spacing={1.5}>
              {/* Order Number */}
              <Box>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{
                    textTransform: 'uppercase',
                    letterSpacing: 0.5,
                    fontWeight: 600,
                    fontSize: '0.7rem'
                  }}
                  component="div"
                >
                  {t('purchase:purchase.orderNumber')}
                </Typography>
                <Typography
                  variant="body1"
                  sx={{
                    fontFamily: 'monospace',
                    fontWeight: 600,
                    fontSize: '0.95rem',
                    mt: 0.5,
                    wordBreak: 'break-all'
                  }}
                  component="div"
                >
                  {order?.uuid}
                </Typography>
              </Box>

              {/* Divider */}
              <Box sx={{
                height: 1,
                background: theme.palette.mode === 'dark'
                  ? 'rgba(255, 255, 255, 0.08)'
                  : 'rgba(0, 0, 0, 0.08)',
              }} />

              {/* Payment Amount - Prominent */}
              <Box>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{
                    textTransform: 'uppercase',
                    letterSpacing: 0.5,
                    fontWeight: 600,
                    fontSize: '0.7rem'
                  }}
                  component="div"
                >
                  {t('purchase:purchase.paymentAmount')}
                </Typography>
                <Stack direction="row" alignItems="baseline" spacing={0.5} sx={{ mt: 0.5 }}>
                  <Typography
                    variant="h3"
                    sx={{
                      fontWeight: 800,
                      fontSize: '2.25rem',
                      background: colors.errorGradient,
                      WebkitBackgroundClip: 'text',
                      WebkitTextFillColor: 'transparent',
                      backgroundClip: 'text',
                      lineHeight: 1,
                    }}
                    component="span"
                  >
                    ${((order?.payAmount ?? 0) / 100).toFixed(2)}
                  </Typography>
                  <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ fontWeight: 500 }}
                    component="span"
                  >
                    USD
                  </Typography>
                </Stack>
              </Box>
            </Stack>
          </Card>

          {/* Confirmation Hint */}
          <Box
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 1,
              p: 1.5,
              borderRadius: 2,
              background: theme.palette.mode === 'dark'
                ? 'rgba(66, 165, 245, 0.08)'
                : 'rgba(66, 165, 245, 0.05)',
              border: '1px solid',
              borderColor: theme.palette.mode === 'dark'
                ? 'rgba(66, 165, 245, 0.2)'
                : 'rgba(66, 165, 245, 0.15)',
            }}
          >
            <WarningIcon
              sx={{
                color: 'primary.main',
                fontSize: 20,
                flexShrink: 0,
                mt: 0.25,
              }}
            />
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{
                fontSize: '0.875rem',
                lineHeight: 1.5,
              }}
              component="span"
            >
              {t('purchase:purchase.paymentConfirmHint')}
            </Typography>
          </Box>
        </Stack>
      </DialogContent>

      {/* Action Buttons */}
      <DialogActions
        sx={{
          px: 3,
          pb: 3,
          pt: 1,
          gap: 1.5,
          justifyContent: 'stretch',
        }}
      >
        <Button
          variant="contained"
          color="success"
          onClick={onSuccess}
          startIcon={<CheckCircleIcon />}
          fullWidth
          sx={{
            py: 1.5,
            fontWeight: 700,
            fontSize: '1rem',
            borderRadius: 2,
            textTransform: 'none',
            boxShadow: colors.successGlow,
            transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
            '&:hover': {
              transform: 'translateY(-2px)',
              boxShadow: `${colors.successGlow}, 0 8px 16px rgba(76, 175, 80, 0.3)`,
            },
            '&:active': {
              transform: 'translateY(0)',
            },
          }}
        >
          {t('purchase:purchase.paymentSuccess')}
        </Button>
        <Button
          variant="outlined"
          color="error"
          onClick={onFail}
          startIcon={<ErrorIcon />}
          fullWidth
          sx={{
            py: 1.5,
            fontWeight: 700,
            fontSize: '1rem',
            borderRadius: 2,
            borderWidth: 2,
            textTransform: 'none',
            transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
            '&:hover': {
              borderWidth: 2,
              transform: 'translateY(-2px)',
              boxShadow: '0 4px 12px rgba(244, 67, 54, 0.2)',
            },
            '&:active': {
              transform: 'translateY(0)',
            },
          }}
        >
          {t('purchase:purchase.paymentFailed')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// Tier 选择器 - 当有多个 tier 时显示，单一 tier 时隐藏
function TierSelector({
  tiers,
  tierGroups,
  selected,
  onSelect,
}: {
  tiers: string[];
  tierGroups: Map<string, Plan[]>;
  selected: string;
  onSelect: (tier: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <Stack direction="row" spacing={1} sx={{ mb: 2, flexWrap: 'wrap' }}>
      {tiers.map(tier => {
        const isSelected = selected === tier;
        const plansInTier = tierGroups.get(tier) || [];
        const minPrice = Math.min(...plansInTier.map(p => p.price / p.month));
        return (
          <Chip
            key={tier}
            label={`${t(`purchase:tier.${tier}`, tier)}  $${(minPrice / 100).toFixed(0)}/mo`}
            variant={isSelected ? 'filled' : 'outlined'}
            color={isSelected ? 'primary' : 'default'}
            onClick={() => onSelect(tier)}
            sx={{
              height: 36,
              borderRadius: 2,
              cursor: 'pointer',
              fontWeight: isSelected ? 700 : 500,
              '&:hover': { borderColor: 'primary.main' },
            }}
          />
        );
      })}
    </Stack>
  );
}

// 套餐列表组件 - 使用 memo 优化，只在 plans/selectedPlan 变化时重新渲染
const PlanList = memo(function PlanList({
  plans,
  selectedPlan,
  onSelect,
}: {
  plans: Plan[];
  selectedPlan: string;
  onSelect: (pid: string) => void;
}) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  return (
    <Stack direction="column" spacing={1.5}>
      {plans
        .slice()
        .sort((a, b) => a.month - b.month)
        .map((item) => {
          const discount = item.originPrice > item.price ? (item.originPrice - item.price) / 100 : 0;
          const isSelected = selectedPlan === item.pid;
          return (
            <Card
              key={item.pid}
              variant="outlined"
              sx={(theme) => ({
                borderColor: isSelected ? colors.selectedBorder : theme.palette.mode === 'dark' ? 'rgba(255, 255, 255, 0.12)' : "grey.300",
                borderWidth: isSelected ? 3 : 2,
                borderStyle: "solid",
                borderRadius: 3,
                boxShadow: isSelected
                  ? colors.selectedShadow
                  : '0 2px 8px rgba(0, 0, 0, 0.08)',
                background: isSelected
                  ? colors.selectedGradient
                  : theme.palette.background.paper,
                cursor: "pointer",
                transition: "all 0.3s cubic-bezier(0.4, 0, 0.2, 1)",
                p: 2,
                minHeight: 80,
                position: 'relative',
                overflow: 'visible',
                '&:hover': {
                  borderColor: "primary.main",
                  boxShadow: colors.selectedShadow,
                  transform: 'translateY(-4px) scale(1.01)',
                },
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
              })}
              onClick={() => onSelect(item.pid)}
            >
              {item.highlight && <Ribbon text={t('purchase:purchase.hotPlan')} />}

              {/* 左侧：Radio+套餐信息 */}
              <Stack direction="row" alignItems="center" spacing={2} sx={{ flex: 1, minWidth: 0 }}>
                <Box
                  sx={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: 48,
                    height: 48,
                    borderRadius: '50%',
                    bgcolor: isSelected
                      ? 'primary.main'
                      : colors.disabledBg,
                    transition: 'all 0.3s',
                    flexShrink: 0,
                  }}
                >
                  {isSelected ? (
                    <CheckCircleIcon sx={{ color: 'white', fontSize: 28 }} />
                  ) : (
                    <Radio
                      checked={false}
                      value={item.pid}
                      color="primary"
                      sx={{ p: 0 }}
                    />
                  )}
                </Box>
                <Box sx={{ minWidth: 0 }}>
                  <Typography
                    variant="h6"
                    fontWeight={700}
                    noWrap
                    sx={{
                      fontSize: '1.1rem',
                      color: isSelected ? 'primary.main' : 'text.primary',
                      mb: 0.5,
                    }}
                    component="div"
                  >
                    {t(`purchase:plan.pid.${item.pid}`)}
                  </Typography>
                  <Stack direction="row" alignItems="baseline" spacing={0.5}>
                    <Typography variant="h4" color="primary.main" fontWeight={800} component="span">
                      ${ (item.price / item.month / 100).toFixed(2) }
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.9rem' }} component="span">
                      /{t('purchase:purchase.month')}
                    </Typography>
                  </Stack>
                </Box>
              </Stack>

              {/* 右侧：价格信息 */}
              <Stack alignItems="flex-end" spacing={0.8} sx={{ minWidth: 0, ml: 2 }}>
                <Stack direction="row" alignItems="center" spacing={1}>
                  <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500 }} component="span">
                    {t('purchase:purchase.totalPrice')}
                  </Typography>
                  <Typography variant="h6" color="error.main" fontWeight={700} component="span">
                    ${ (item.price / 100).toFixed(2) }
                  </Typography>
                </Stack>
                <Stack direction="row" alignItems="center" spacing={1}>
                  <Typography
                    variant="body2"
                    color="text.disabled"
                    sx={{
                      textDecoration: "line-through",
                      fontSize: '0.85rem',
                      fontWeight: 500,
                    }}
                    component="span"
                  >
                    ${ (item.originPrice / 100).toFixed(2) }
                  </Typography>
                  {discount > 0 && (
                    <Chip
                      label={t('purchase:purchase.saveAmount', { amount: discount.toFixed(2) })}
                      color="success"
                      size="small"
                      sx={{
                        fontWeight: 700,
                        fontSize: '0.75rem',
                        height: '24px',
                        boxShadow: colors.successGlow,
                      }}
                    />
                  )}
                </Stack>
              </Stack>
            </Card>
          );
        })}
    </Stack>
  );
});

export default function Purchase() {
  const navigate = useNavigate();
  const { user, isExpired, isMembership, fetchUser } = useUser();
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  // Auth state for login check
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const openLoginDialog = useLoginDialogStore((s) => s.open);

  // Purchase scope, bound to a product line. The default /purchase page sells
  // the app subscription; /purchase?product=private_node sells dedicated lines
  // (with a region picker). The server is the boundary: each scope fetches its
  // own product endpoint, so private_node plans can never reach the default
  // page (incl. already-deployed old clients, which only ever call /api/plans).
  // Entry to the dedicated-line scope is the "buy a line" CTA on /private-node.
  const [searchParams] = useSearchParams();
  const purchaseProduct = searchParams.get('product') === 'private_node' ? 'private_node' : 'app';
  // /api/plans is frozen as the app-only legacy endpoint; new product scopes use
  // the nested /api/products/:product/plans. Cache key is per product.
  const plansPath = purchaseProduct === 'private_node' ? '/api/products/private_node/plans' : '/api/plans';
  const plansCacheKey = purchaseProduct === 'private_node' ? 'api:products:private_node:plans' : 'api:plans';

  const [plan, setPlan] = useState("");
  // 专属节点购买时选定的地区（仅 private_node 套餐使用；shared 套餐留空）。
  const [selectedRegion, setSelectedRegion] = useState("");
  const [showCampaign, setShowCampaign] = useState(false);
  const [campaignCode, setCampaignCode] = useState("");
  const [orderData, setOrderData] = useState<Order | null>(null);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(true); // 默认 loading，等待 API
  const [isLoading, setIsLoading] = useState(false);
  const [campaignError, setCampaignError] = useState<string>("");
  const [payDialogOpen, setPayDialogOpen] = useState(false);
  // iOS StoreKit IAP: when present, the whole purchase screen is replaced by the
  // inline IosSubscribePanel / IosMembershipPanel (Apple 3.1.1 — no external
  // payment, single auto-renewable product, no multi-plan list). The WordGate
  // order/preview flow below never runs on iOS.
  const iap = window._platform?.iap;
  const affordance = useSubscriptionAffordance();
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [, setAppConfigLoading] = useState(false);

  const {showAlert} = useAlert();

  // Tier grouping — kept for the (currently hidden) multi-tier chip selector.
  // Repeat-buyer tier lock is enforced by `filteredPlans` below regardless of
  // selector visibility.
  const [selectedTier, setSelectedTier] = useState('');

  // `plans` already holds exactly the active product's plans — the server
  // filters by endpoint, so no client-side product filter is needed here.
  const tierGroups = useMemo(() => {
    const groups = new Map<string, Plan[]>();
    for (const p of plans) {
      const tier = p.tier || 'basic';
      if (!groups.has(tier)) groups.set(tier, []);
      groups.get(tier)!.push(p);
    }
    return groups;
  }, [plans]);

  const tiers = useMemo(() => [...tierGroups.keys()], [tierGroups]);
  // Manual tier picker not yet released — repeat buyers are auto-filtered to
  // their existing tier, first-time buyers see every tier as one flat list.
  const showTierSelector = false;

  // Auto-select tier when plans load
  useEffect(() => {
    if (!tiers.length) return;
    if (selectedTier && tiers.includes(selectedTier)) return;
    const highlightedPlan = plans.find(p => p.highlight);
    setSelectedTier(highlightedPlan?.tier || tiers[0]);
  }, [tiers, plans, selectedTier]);

  // Filter plans by user tier (Plan A):
  //   - First-time buyer (or unauthenticated browse): show every plan.
  //   - Repeat buyer: lock to plans matching `user.tier`. Backend enforces
  //     the same rule and returns TIER_MISMATCH (422001) on violation.
  const filteredPlans = useMemo(() => {
    if (showTierSelector) {
      return tierGroups.get(selectedTier) || plans;
    }
    if (!user?.isFirstOrderDone) {
      return plans;
    }
    return plans.filter(p => p.tier === user.tier);
  }, [showTierSelector, selectedTier, tierGroups, plans, user]);

  // When tier changes, ensure selected plan is valid in the new tier
  useEffect(() => {
    if (!filteredPlans.length) return;
    const currentInTier = filteredPlans.find(p => p.pid === plan);
    if (currentInTier) return;
    const highlighted = filteredPlans.find(p => p.highlight);
    setPlan(highlighted?.pid || filteredPlans[0].pid);
  }, [filteredPlans, plan]);

  // 当前选中的套餐对象（用于专属节点的地区/IP/流量展示）。
  const selectedPlanObj = useMemo(
    () => plans.find(p => p.pid === plan) ?? null,
    [plans, plan],
  );
  const isPrivateNode = selectedPlanObj?.product === 'private_node';
  const allowedRegions = selectedPlanObj?.privateNode?.allowedRegions ?? [];

  // 选中专属节点套餐时，默认选第一个可选地区；切回共享套餐时清空。
  useEffect(() => {
    if (!isPrivateNode) {
      if (selectedRegion) setSelectedRegion("");
      return;
    }
    if (allowedRegions.length === 0) {
      setSelectedRegion("");
      return;
    }
    if (!allowedRegions.includes(selectedRegion)) {
      setSelectedRegion(allowedRegions[0]);
    }
  }, [isPrivateNode, allowedRegions, selectedRegion]);

  // 处理订单创建（使用 useCallback 避免不必要的重新渲染）
  const handleOrder = useCallback(async ({preview = false}: {preview?: boolean}) => {
    // 非预览模式需要登录才能创建订单
    if (!preview && !isAuthenticated) {
      openLoginDialog({
        trigger: 'purchase',
        message: t('auth:auth.startNowHint'),
      });
      return;
    }

    setIsLoading(true);
    try {
      // 专属节点套餐需带上选定地区；共享套餐留空（Center 忽略）。
      const region = isPrivateNode ? (selectedRegion || undefined) : undefined;
      console.info('[Purchase] 创建订单请求: ' + JSON.stringify({ preview, plan, campaignCode, region }));
      const response = await cloudApi.post<{ order: Order; payUrl?: string }>('/api/user/orders', {
        preview,
        plan,
        campaignCode: campaignCode || undefined,
        region,
      });
      console.info('[Purchase] 创建订单响应: ' + JSON.stringify(response));
      
      if (response.code === 0 && response.data) {
        // 统一处理订单数据，无论是预览还是实际订单
        const { order, payUrl } = response.data;
        setOrderData(order);
        setCampaignError(""); // 清除之前的错误
        
        // 只有非预览模式才进行支付相关操作。iOS 不会走到这里（整页被 IAP 面板替代），
        // 其余平台（web / desktop / Android）保持原有 WordGate 外链路径。
        if (!preview && payUrl) {
          setPayDialogOpen(true);
          window._platform!.openExternal?.(payUrl);
        }
      } else {
        // 统一错误处理逻辑
        if (response.code === ERROR_CODES.INVALID_CAMPAIGN_CODE) {
          console.error('[Purchase] Invalid campaign code:', response.code, response.message);
          setCampaignError(t('purchase:purchase.invalidCampaignCode'));
          // 优惠码错误时不清除订单数据，保持当前预览状态
        } else if (response.code === ERROR_CODES.TIER_MISMATCH) {
          // Tier 锁定：仅同档续费，跨档需联系客服。后端在首单/续费两路都校验。
          console.warn('[Purchase] Tier mismatch:', response.code, response.message);
          if (!preview) {
            setCampaignError("");
            setOrderData(null);
            showAlert(
              t('purchase:purchase.tierLocked', {
                tier: user?.tier ?? 'basic',
              }),
              'error'
            );
          }
        } else if (response.code === ERROR_CODES.PROXY_PURCHASE_DEPRECATED) {
          // 代付已下线 — UI 已不发送此请求，仅兜底陈旧客户端。
          console.warn('[Purchase] Proxy purchase deprecated:', response.code, response.message);
          if (!preview) {
            setCampaignError("");
            setOrderData(null);
            showAlert(getErrorMessage(response.code, response.message, t), 'error');
          }
        } else if (response.code === ERROR_CODES.INVALID_ARGUMENT) {
          // 专属节点地区非法等参数错误（Center 返回 422 ErrorInvalidArgument）。
          console.warn('[Purchase] Invalid argument:', response.code, response.message);
          if (!preview) {
            setCampaignError("");
            setOrderData(null);
            showAlert(getErrorMessage(response.code, response.message, t), 'error');
          }
        } else {
          // 只在非预览模式下清除状态和显示错误提示
          if (!preview) {
            console.error('[Purchase] Create order failed:', response.code, response.message);
            setCampaignError("");
            setOrderData(null);
            showAlert(t('purchase:purchase.createOrderFailed'), "error");
          }
        }
      }
    } catch (error) {
      console.error(`[Purchase] ${preview ? '预览' : '创建'}订单异常:`, error);

      // 只在非预览模式下清除订单数据和优惠码错误
      if (!preview) {
        setCampaignError("");
        setOrderData(null);
        // 显示具体的错误信息，连接问题由 BridgeContext 统一处理
        showAlert(t('purchase:purchase.operationFailed'), "error");
      }
    } finally {
      setIsLoading(false);
    }
  }, [plan, campaignCode, showAlert, t, isAuthenticated, openLoginDialog, user, isPrivateNode, selectedRegion]);

  // 用于标记是否已选择过默认套餐（避免 plan 变化触发重新获取套餐列表）
  const defaultPlanSelectedRef = useRef(false);

  // 获取套餐列表：使用 k2api 缓存（SWR 模式 + 过期缓存 fallback）
  useEffect(() => {
    const selectDefaultPlan = (planList: Plan[]) => {
      // planList is already scoped to the active product by the endpoint.
      if (planList.length > 0 && !defaultPlanSelectedRef.current) {
        defaultPlanSelectedRef.current = true;
        const highlightedPlan = planList.find((p: { highlight?: boolean }) => p.highlight);
        const defaultPlan = highlightedPlan ? highlightedPlan.pid : planList[0].pid;
        console.info('[Purchase] 选择默认套餐:', defaultPlan, highlightedPlan ? '(热门)' : '(第一个)');
        setPlan(defaultPlan);
      }
    };

    const fetchPlans = async () => {
      setPlansLoading(true);
      try {
        console.info('[Purchase] 开始获取套餐列表');
        // 使用 k2api 缓存：
        // - key: 缓存键
        // - ttl: 5分钟缓存
        // - revalidate: SWR 模式，立即返回缓存同时后台刷新
        // - allowExpired: 网络失败时使用过期缓存
        // Check cache first (SWR: return cache immediately, refresh in background)
        const cachedPlans = cacheStore.get<{ items: Plan[] }>(plansCacheKey);
        if (cachedPlans) {
          const fetchedPlans = cachedPlans.items || [];
          setPlans(fetchedPlans);
          selectDefaultPlan(fetchedPlans);
          setPlansLoading(false);
          // Background revalidate
          cloudApi.get<{ items: Plan[] }>(plansPath).then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set(plansCacheKey, res.data, { ttl: 300 });
              const updatedPlans = res.data.items || [];
              setPlans(updatedPlans);
              selectDefaultPlan(updatedPlans);
            }
          });
          return;
        }

        const response = await cloudApi.get<{ items: Plan[] }>(plansPath);

        if (response.code === 0 && response.data) {
          cacheStore.set(plansCacheKey, response.data, { ttl: 300 });
          const fetchedPlans = response.data.items || [];
          setPlans(fetchedPlans);
          selectDefaultPlan(fetchedPlans);
        } else {
          console.error('[Purchase] Failed to get plans list:', response.code, response.message);
          showAlert(t('purchase:purchase.getPlansListFailed'), "error");
        }
      } catch (error) {
        console.error('[Purchase] 获取套餐列表异常:', error);
        showAlert(t('purchase:purchase.operationFailed'), "error");
      } finally {
        setPlansLoading(false);
      }
    };

    fetchPlans();
  }, [showAlert, t, isAuthenticated, plansPath, plansCacheKey]); // 登录后 / 购买范围(产品)变化时重新加载套餐列表

  // 获取 App 配置（邀请奖励信息）
  useEffect(() => {
    const fetchAppConfig = async () => {
      setAppConfigLoading(true);
      try {
        console.info('[Purchase] 获取 App 配置');
        // Use k2api cache: app config rarely changes
        // - key: cache key
        // - ttl: 10 minutes cache
        // - revalidate: SWR mode, return cache immediately and refresh in background
        // - allowExpired: use expired cache as fallback on network failure
        // Check cache first (SWR: return cache immediately, refresh in background)
        const cachedConfig = cacheStore.get<AppConfig>('api:app_config');
        if (cachedConfig) {
          setAppConfig(cachedConfig);
          setAppConfigLoading(false);
          // Background revalidate
          cloudApi.get<AppConfig>('/api/app/config').then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set('api:app_config', res.data, { ttl: 600 });
              setAppConfig(res.data);
            }
          });
          return;
        }

        const response = await cloudApi.get<AppConfig>('/api/app/config');
        console.info('[Purchase] App 配置响应: ' + JSON.stringify(response));

        if (response.code === 0 && response.data) {
          cacheStore.set('api:app_config', response.data, { ttl: 600 });
          setAppConfig(response.data);
        } else {
          console.error('[Purchase] 获取 App 配置失败:', response.message);
        }
      } catch (error) {
        console.error('[Purchase] 获取 App 配置异常:', error);
      } finally {
        setAppConfigLoading(false);
      }
    };

    fetchAppConfig();
  }, []);

  // 当计划或优惠码变化时，重新获取预览数据
  useEffect(() => {
    if (plan && !plansLoading) {
      console.info('[Purchase] 触发预览订单: ' + JSON.stringify({ plan, campaignCode }));
      handleOrder({preview: true});
    }
  }, [campaignCode, plan, plansLoading, handleOrder]);

  // 处理套餐选择（使用 useCallback 保证引用稳定，避免 PlanList 不必要的重新渲染）
  const handlePlanSelect = useCallback((pid: string) => {
    setPlan(pid);
  }, []);

  const handlePaySuccess = async () => {
    setPayDialogOpen(false);
    // 刷新用户资料
    try {
      await fetchUser(true);
    } catch (error) {
      console.error(t('purchase:purchase.refreshUserProfileFailed'), error);
    }
    // 跳转到付费和授权历史页面
    navigate('/pro-histories?from=/purchase');
  };
  const handlePayFail = async () => {
    setPayDialogOpen(false);
    // 刷新用户资料
    try {
      await fetchUser(true);
    } catch (error) {
      console.error(t('purchase:purchase.refreshUserProfileFailed'), error);
    }
    // 跳转到付费和授权历史页面
    navigate('/pro-histories?from=/purchase');
  };

  // iOS 订阅轨完全绕开 WordGate 多套餐购买流（单一自动续订商品，Apple 3.1.1）。
  //   - manage/status → 会员面板（管理订阅 / 显示到期）。
  //   - subscribe（含未登录潜客）→ 内联订阅面板：权益 + 单一商品 + 订阅按钮，无弹窗。
  if (iap) {
    if (affordance.mode !== 'subscribe') {
      return (
        <IosMembershipPanel
          mode={affordance.mode as 'manage' | 'status'}
          activeSub={affordance.activeSub}
        />
      );
    }
    // 单一 basic 商品的权益上限（各时长一致）；缺数据时 MembershipBenefits 走默认值。
    const iapPlan =
      plans.find((p) => p.tier === 'basic') ??
      plans.find((p) => p.pid === plan) ??
      plans[0];
    return (
      <IosSubscribePanel
        isAuthenticated={isAuthenticated}
        accountToken={user?.appleAccountToken ?? ''}
        isMembership={isMembership}
        isExpired={isExpired}
        maxDevice={iapPlan?.maxDevice}
        maxRouterDevice={iapPlan?.maxRouterDevice}
        maxLanClient={iapPlan?.maxLanClient}
      />
    );
  }

  // Stripe 订阅品牌（web/desktop）：整页替换为订阅/管理面板。
  // 纯订阅模式——WordGate 订单/campaign/专属节点流对该品牌永不运行。
  if (!iap && brandConfig.features.stripeCheckout) {
    return <StripePurchasePanel plans={plans} plansLoading={plansLoading} />;
  }

  // Brand payment-channel gate: without WordGate (web/desktop flow) and
  // without IAP (iOS), this brand has no in-app purchase channel yet.
  // Point users at the website. Now only covers the hypothetical brand shape
  // with both gates off (fail-safe) — overleap is handled above.
  if (!iap && !brandConfig.features.wordgatePurchase) {
    return (
      <Box sx={{ p: 4, textAlign: 'center' }}>
        <Typography variant="h6" sx={{ mb: 2 }}>
          {t('purchase:purchase.paymentChannelUnavailable')}
        </Typography>
        <Button
          variant="contained"
          onClick={() => window._platform?.openExternal?.(`${brandConfig.baseURL}/purchase`)}
        >
          {t('purchase:purchase.buyOnWebsite')}
        </Button>
      </Box>
    );
  }

  return (
    <Box sx={{
      width: "100%",
      py: 1,
      backgroundColor: "transparent"
    }}>
      <Stack direction="column" spacing={2.5}>
        {/* 根据用户状态显示相应提示 */}
        {!isMembership ? (
          <Box sx={{
            mb: 1,
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
            mb: 1,
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

        {/* 会员权益 — 先展示价值，再要求行动 */}
        <MembershipBenefits
          maxDevice={plans.find(p => p.pid === plan)?.maxDevice}
          maxRouterDevice={plans.find(p => p.pid === plan)?.maxRouterDevice}
          maxLanClient={plans.find(p => p.pid === plan)?.maxLanClient}
        />

        {/* 登录/注册 */}
        {!isAuthenticated && (
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
              {t('purchase:purchase.bindEmail')}
            </Typography>
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <EmailLoginForm onLoginSuccess={() => {
                console.info('[Purchase] Login success, refreshing user info');
                // User info will be refreshed automatically
              }} />
            </Card>
          </Box>
        )}

        {/* 邀请奖励横幅 */}
        {user?.inviteCode && appConfig?.inviteReward && (
          <Box sx={{
            mb: 1,
            borderRadius: 2,
            boxShadow: 2,
            background: colors.warningGradient,
            p: 1.5,
            display: 'flex',
            alignItems: 'center',
          }}>
            <EmojiEventsIcon sx={{ color: colors.warningDark, fontSize: 28, mr: 1.5 }} />
            <Typography variant="subtitle1" fontWeight="bold" color="text.primary" sx={{ fontSize: '1rem' }} component="span" >
              {t('purchase:purchase.friendReferralBonus', { days: appConfig.inviteReward.inviterPurchaseRewardDays })}
            </Typography>
          </Box>
        )}

        {/* Plan 选择（纵向排列） */}
        <Box>
          <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">{t('purchase:purchase.selectPlan')}</Typography>

          {/* Tier selector — only shown when multiple tiers exist */}
          {showTierSelector && !plansLoading && (
            <TierSelector
              tiers={tiers}
              tierGroups={tierGroups}
              selected={selectedTier}
              onSelect={setSelectedTier}
            />
          )}

          {plansLoading ? (
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <LoadingState message={t('purchase:purchase.loading')} minHeight={200} />
            </Card>
          ) : plans.length === 0 ? (
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <EmptyPlans />
            </Card>
          ) : filteredPlans.length === 0 ? (
            // Repeat buyer whose tier currently has no purchasable plans —
            // show the tier-locked message so they know to contact support.
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center', py: 2 }}>
                {t('purchase:purchase.tierLocked', {
                  tier: user?.tier ?? 'basic',
                })}
              </Typography>
            </Card>
          ) : (
            <PlanList
              plans={filteredPlans}
              selectedPlan={plan}
              onSelect={handlePlanSelect}
            />
          )}
        </Box>

        {/* 专属节点：地区选择 + IP 类型 + 流量配额 */}
        {!plansLoading && isPrivateNode && selectedPlanObj?.privateNode && (
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
              {t('privateNode:privateNode.title')}
            </Typography>
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <Stack spacing={2}>
                {/* 地区选择：多地区用 Select，单地区只读展示 */}
                {allowedRegions.length > 1 ? (
                  <FormControl fullWidth size="small">
                    <InputLabel id="private-node-region-label">
                      {t('privateNode:privateNode.selectRegion')}
                    </InputLabel>
                    <Select
                      labelId="private-node-region-label"
                      label={t('privateNode:privateNode.selectRegion')}
                      value={selectedRegion}
                      onChange={(e) => setSelectedRegion(e.target.value)}
                    >
                      {allowedRegions.map((r) => (
                        <MenuItem key={r} value={r}>{r}</MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                ) : (
                  <Stack direction="row" justifyContent="space-between" alignItems="center">
                    <Typography variant="body2" color="text.secondary" component="span">
                      {t('privateNode:privateNode.region')}
                    </Typography>
                    <Typography variant="body2" fontWeight={600} component="span">
                      {allowedRegions[0] ?? ''}
                    </Typography>
                  </Stack>
                )}

                {/* IP 类型 */}
                <Stack direction="row" justifyContent="space-between" alignItems="center">
                  <Typography variant="body2" color="text.secondary" component="span">
                    {t('privateNode:privateNode.ipTypeLabel')}
                  </Typography>
                  <Typography variant="body2" fontWeight={600} component="span">
                    {t(`privateNode:privateNode.ipType.${selectedPlanObj.privateNode.ipType}`)}
                  </Typography>
                </Stack>

                {/* 流量配额 */}
                <Stack direction="row" justifyContent="space-between" alignItems="center">
                  <Typography variant="body2" color="text.secondary" component="span">
                    {t('privateNode:privateNode.quota')}
                  </Typography>
                  <Typography variant="body2" fontWeight={600} component="span">
                    {formatBytes(selectedPlanObj.privateNode.trafficTotalBytes)}
                  </Typography>
                </Stack>
              </Stack>
            </Card>
          </Box>
        )}

        {/* 总价展示 - 只有在有套餐数据时才显示 */}
        {!plansLoading && plans.length > 0 && (
          <Box sx={{
            mt: 2,
            p: 2.5,
            background: colors.selectedGradient,
            borderRadius: 3,
            border: '2px solid',
            borderColor: 'primary.main',
            boxShadow: colors.selectedShadow,
          }}>
            <Stack direction="row" justifyContent="space-between" alignItems="flex-start">
              <Box>
                <Typography variant="subtitle1" fontWeight="bold" component="span">
                  {t('purchase:purchase.total')}
                </Typography>
                <Typography variant="body2" color="primary" sx={{ mt: 0.5, display: 'flex', alignItems: 'center' }} component="span" >
                  {(() => {
                    const planMonths = plans.find(p => p.pid === plan)?.month || 0;
                    return t('purchase:purchase.memberAuthorization', { months: planMonths });
                  })()}
                  {user?.inviteCode && appConfig?.inviteReward && (
                    <Chip label={t('purchase:purchase.friendReferralGift', { days: appConfig.inviteReward.purchaseRewardDays })} color="success" size="small" sx={{ ml: 1, fontWeight: 'bold' }} component="span" />
                  )}
                </Typography>
                {orderData?.campaign && (
                  <Box sx={{ mt: 1 }}>
                    <Typography variant="body2" color="success.main" sx={{ fontWeight: 'bold' }} component="span">
                      {orderData.campaign.description}
                    </Typography>
                  </Box>
                )}
                {!showCampaign && !campaignCode && (
                  <Button
                    startIcon={<AddIcon />}
                    size="small"
                    sx={{ 
                      mt: 1, 
                      textTransform: "none", 
                      fontWeight: "bold",
                      color: 'primary.main',
                      '&:hover': {
                        bgcolor: 'rgba(25, 118, 210, 0.08)',
                      }
                    }}
                    onClick={() => setShowCampaign(true)}
                  >
                    {t('purchase:purchase.havePromoCode')}
                  </Button>
                )}
                <Collapse in={showCampaign}>
                  <Box sx={{ mt: 1 }}>
                    <TextField
                      fullWidth
                      size="small"
                      placeholder={t('purchase:purchase.campaignCodePlaceholder')}
                      value={campaignCode}
                      onChange={(e) => {
                        setCampaignCode(e.target.value);
                        setCampaignError(""); // 清除错误当用户输入时
                      }}
                      error={!!campaignError}
                      inputProps={{
                        autoCapitalize: "characters",
                        autoCorrect: "off",
                        autoComplete: "off",
                        spellCheck: false,
                      }}
                      InputProps={{
                        endAdornment: campaignCode && (
                          <InputAdornment position="end">
                            <Button
                              size="small"
                              onClick={() => {
                                setCampaignCode('');
                                setCampaignError('');
                                setShowCampaign(false);
                              }}
                              sx={{
                                minWidth: 'auto',
                                color: 'text.secondary',
                                '&:hover': {
                                  color: 'error.main',
                                }
                              }}
                            >
                              {t('purchase:purchase.clear')}
                            </Button>
                          </InputAdornment>
                        ),
                      }}
                    />
                    {campaignError && (
                      <Box sx={{ mt: 0.5, display: 'flex', alignItems: 'center' }}>
                        <ErrorIcon sx={{ color: 'error.main', fontSize: 16, mr: 0.5 }} />
                        <Typography variant="body2" color="error" sx={{ fontWeight: 'bold' }} component="span">
                          {campaignError}
                        </Typography>
                      </Box>
                    )}
                  </Box>
                </Collapse>
              </Box>
              <Box sx={{ textAlign: 'right' }}>
                {orderData?.originAmount && orderData.originAmount > orderData.payAmount && (
                  <Typography variant="body2" color="text.disabled" sx={{ textDecoration: 'line-through' }} component="span">
                    ${ (orderData.originAmount / 100).toFixed(2) }
                  </Typography>
                )}
                <Typography variant="h5" color="error" fontWeight="bold" component="span">
                  ${ ((orderData?.payAmount ?? plans.find(p => p.pid === plan)?.price ?? 0) / 100).toFixed(2) }
                </Typography>
                <Typography variant="body2" color="text.secondary" component="span">
                  {t('purchase:purchase.includingTax')}
                </Typography>
              </Box>
            </Stack>
          </Box>
        )}

        <Button
          variant="contained"
          color="error"
          size="large"
          fullWidth
          onClick={() => handleOrder({ preview: false })}
          disabled={plansLoading || plans.length === 0 || filteredPlans.length === 0 || !plan || isLoading || !isAuthenticated}
          sx={{
            fontWeight: 700,
            fontSize: 18,
            py: 1.75,
            mt: 2,
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
            '&:active': {
              transform: 'translateY(0)',
            },
            '&:disabled': {
              background: colors.disabledGradient,
              boxShadow: 'none',
            },
          }}
        >
          {plansLoading || isLoading ? t('purchase:purchase.loadingPlans') :
           plans.length === 0 ? t('purchase:purchase.noPlans') :
           !isAuthenticated ? t('purchase:purchase.bindEmailToPayNow') :
           t('purchase:purchase.payNow')}
        </Button>
        
        {/* 支付结果对话框 */}
        <PayResultDialog
          open={payDialogOpen}
          order={orderData}
          onSuccess={handlePaySuccess}
          onFail={handlePayFail}
        />
      </Stack>
    </Box>
  );
}
