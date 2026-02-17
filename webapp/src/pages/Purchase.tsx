import { useState, useEffect, useCallback, memo, useRef } from "react";
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
  useTheme,
} from "@mui/material";
import { Add as AddIcon, EmojiEvents as EmojiEventsIcon, Error as ErrorIcon } from "@mui/icons-material";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAlert, useAuthStore } from "../stores";
import { useUser } from "../hooks/useUser";
import { useLoginDialogStore } from "../stores/login-dialog.store";

import { type Plan, type Order, type AppConfig, ErrorInvalidCampaignCode } from "../services/api-types";
import { LoadingState, EmptyPlans } from '../components/LoadingAndEmpty';
import MemberSelection from '../components/MemberSelection';
import EmailLoginForm from '../components/EmailLoginForm';
import {
  Warning as WarningIcon,
  CheckCircle as CheckCircleIcon,
} from "@mui/icons-material";
import { getThemeColors } from '../theme/colors';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';

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

  const [plan, setPlan] = useState("");
  const [showCampaign, setShowCampaign] = useState(false);
  const [campaignCode, setCampaignCode] = useState("");
  const [orderData, setOrderData] = useState<Order | null>(null);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(true); // 默认 loading，等待 API
  const [isLoading, setIsLoading] = useState(false);
  const [campaignError, setCampaignError] = useState<string>("");
  const [payDialogOpen, setPayDialogOpen] = useState(false);
  const [selectedForMyself, setSelectedForMyself] = useState(true);
  const [selectedMemberUUIDs, setSelectedMemberUUIDs] = useState<string[]>([]);
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [, setAppConfigLoading] = useState(false);

  const {showAlert} = useAlert();

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

    // 检查是否有选择付费对象
    if (!selectedForMyself && selectedMemberUUIDs.length === 0) {
      showAlert(t('purchase:purchase.selectAtLeastOneTarget'), "error");
      return;
    }

    setIsLoading(true);
    try {
      console.info('[Purchase] 创建订单请求: ' + JSON.stringify({ preview, plan, campaignCode, selectedForMyself, selectedMemberUUIDs }));
      const response = await cloudApi.post<{ order: Order; payUrl?: string }>('/api/user/orders', {
        preview,
        plan,
        campaignCode: campaignCode || undefined,
        forMyself: selectedForMyself,
        forUserUUIDs: selectedMemberUUIDs.length > 0 ? selectedMemberUUIDs : undefined,
      });
      console.info('[Purchase] 创建订单响应: ' + JSON.stringify(response));
      
      if (response.code === 0 && response.data) {
        // 统一处理订单数据，无论是预览还是实际订单
        const { order, payUrl } = response.data;
        setOrderData(order);
        setCampaignError(""); // 清除之前的错误
        
        // 只有非预览模式才进行支付相关操作
        if (!preview && payUrl) {
          setPayDialogOpen(true);
          window._platform!.openExternal?.(payUrl);
        }
      } else {
        // 统一错误处理逻辑
        if (response.code === ErrorInvalidCampaignCode) {
          console.error('[Purchase] Invalid campaign code:', response.code, response.message);
          setCampaignError(t('purchase:purchase.invalidCampaignCode'));
          // 优惠码错误时不清除订单数据，保持当前预览状态
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
  }, [plan, campaignCode, selectedForMyself, selectedMemberUUIDs, showAlert, t, isAuthenticated, openLoginDialog]);

  // 用于标记是否已选择过默认套餐（避免 plan 变化触发重新获取套餐列表）
  const defaultPlanSelectedRef = useRef(false);

  // 获取套餐列表：使用 k2api 缓存（SWR 模式 + 过期缓存 fallback）
  useEffect(() => {
    const selectDefaultPlan = (planList: Plan[]) => {
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
        const cachedPlans = cacheStore.get<{ items: Plan[] }>('api:plans');
        if (cachedPlans) {
          const fetchedPlans = cachedPlans.items || [];
          setPlans(fetchedPlans);
          selectDefaultPlan(fetchedPlans);
          setPlansLoading(false);
          // Background revalidate
          cloudApi.get<{ items: Plan[] }>('/api/plans').then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set('api:plans', res.data, { ttl: 300 });
              const updatedPlans = res.data.items || [];
              setPlans(updatedPlans);
              selectDefaultPlan(updatedPlans);
            }
          });
          return;
        }

        const response = await cloudApi.get<{ items: Plan[] }>('/api/plans');

        if (response.code === 0 && response.data) {
          cacheStore.set('api:plans', response.data, { ttl: 300 });
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
  }, [showAlert, t, isAuthenticated]); // 登录后重新加载套餐列表

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

  // 当计划、优惠码或选择目标变化时，重新获取预览数据
  useEffect(() => {
    if (plan && !plansLoading && (selectedForMyself || selectedMemberUUIDs.length > 0)) {
      console.info('[Purchase] 触发预览订单: ' + JSON.stringify({ plan, campaignCode, selectedForMyself, selectedMemberUUIDs }));
      handleOrder({preview: true});
    }
  }, [campaignCode, plan, plansLoading, selectedForMyself, selectedMemberUUIDs, handleOrder]);

  // 处理成员选择变化
  const handleMemberSelectionChange = (forMyself: boolean, memberUUIDs: string[]) => {
    setSelectedForMyself(forMyself);
    setSelectedMemberUUIDs(memberUUIDs);
  };

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
    navigate('/pro-histories?type=recharge&from=/purchase');
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
    navigate('/pro-histories?type=recharge&from=/purchase');
  };

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

        {/* Step 1: 登录/注册或成员选择 */}
        <Box>
          {!isAuthenticated ? (
            <>
              <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
                {t('purchase:purchase.bindEmailAndSelectTarget')}
              </Typography>
              <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
                <EmailLoginForm onLoginSuccess={() => {
                  console.info('[Purchase] Login success, refreshing user info');
                  // User info will be refreshed automatically
                }} />
              </Card>
            </>
          ) : (
            <MemberSelection
              selectedForMyself={selectedForMyself}
              selectedMemberUUIDs={selectedMemberUUIDs}
              onSelectionChange={handleMemberSelectionChange}
            />
          )}
        </Box>

        {/* 总价展示前，插入显著横幅 */}
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
          
          {plansLoading ? (
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <LoadingState message={t('purchase:purchase.loading')} minHeight={200} />
            </Card>
          ) : plans.length === 0 ? (
            <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
              <EmptyPlans />
            </Card>
          ) : (
            <PlanList
              plans={plans}
              selectedPlan={plan}
              onSelect={handlePlanSelect}
            />
          )}
        </Box>

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
                    const targetCount = (selectedForMyself ? 1 : 0) + selectedMemberUUIDs.length;
                    if (targetCount === 1) {
                      return t('purchase:purchase.proAuthorization', { months: planMonths });
                    } else {
                      return t('purchase:purchase.proAuthorizationMultiple', { months: planMonths, count: targetCount });
                    }
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
          disabled={plansLoading || plans.length === 0 || !plan || isLoading || !isAuthenticated || (!selectedForMyself && selectedMemberUUIDs.length === 0)}
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
           (!isAuthenticated || (!selectedForMyself && selectedMemberUUIDs.length === 0)) ? t('purchase:purchase.selectTarget') :
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
