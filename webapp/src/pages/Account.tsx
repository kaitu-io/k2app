import { useState, useEffect } from "react";
import {
  Box,
  Typography,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
  ListItemSecondaryAction,
  Chip,
  Button,
  Divider,
  Switch,
  Alert,
  CircularProgress,
  Card,
  CardContent,
  Stack,
  Dialog,
  DialogContent,
  DialogContentText,
  DialogActions,
  IconButton,
  useTheme as useMuiTheme,
} from "@mui/material";
import {
  Email as EmailIcon,
  Devices as DevicesIcon,
  History as HistoryIcon,
  ChevronRight as ChevronRightIcon,
  Logout as LogoutIcon,
  Language as LanguageIcon,
  PrivacyTip as PrivacyTipIcon,
  Group as GroupIcon,
  CardMembership as MembershipIcon,
  PhoneAndroid as PhoneAndroidIcon,
  Feedback as FeedbackIcon,
  Refresh as RefreshIcon,
  ShoppingCart as ShoppingCartIcon,
  CheckCircle as CheckCircleIcon,
  Error as ErrorIcon,
  AccountBalanceWallet as WalletIcon,
  Lock as LockIcon,
  Dns as DnsIcon,
  ExpandMore as ExpandMoreIcon,
  Badge as UserIdIcon,
  ContentCopy as CopyIcon,
} from "@mui/icons-material";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth, useAlert } from "../stores";
import { useUser } from "../hooks/useUser";
import { languages, changeLanguage, normalizeLanguageCode, type LanguageCode } from "../i18n/i18n";
import LanguageDialog from "../components/LanguageDialog";
import { formatDate } from "../utils/time";
import { cloudApi } from "../services/cloud-api";
import { useLoginDialogStore } from "../stores/login-dialog.store";

import type { DataUser } from "../services/api-types";
import { getThemeColors } from '../theme/colors';
import { useAppLinks } from "../hooks/useAppLinks";
import { setAppleConsumptionConsent } from '../components/ios/consumption-consent';
import VersionItem from "../components/VersionItem";
import DeleteAccountDialog from "../components/DeleteAccountDialog";
import BetaChannelToggle from "../components/BetaChannelToggle";
import PasswordDialog from "../components/PasswordDialog";
import { useSubscriptionAffordance } from '../hooks/useSubscriptionAffordance';
import { usePrivateNodes } from '../hooks/usePrivateNodes';
import { getBrandSlogan, getBrandName } from '../brands/i18n-vars';
import { brandConfig } from '../brands';
import { getCurrentAppConfig } from '../config/apps';
import { purchaseSurfaceAvailable } from '../utils/purchase-surface';
import SubscriptionStatusOnly from '../components/SubscriptionStatusOnly';
import { maskEmail } from '../utils/email';

// maskEmail 移到 utils/email（【其他设备安装】也用）；这里保留导出给既有调用方。
export { maskEmail };

export default function Account() {
  const { user, loading, isMembership, isExpired, fetchUser } = useUser();
  const affordance = useSubscriptionAffordance();
  // 复用缓存的 hook（SWR），不给 Account 关键路径增加重 fetch。
  const { nodes: privateNodes } = usePrivateNodes();
  // 专属线路已收口为路由器版官网自助购买——App 内只在用户已有线路时展示入口
  // （不再看 features.privateNode，那是旧的售卖面开关）。
  const showPrivateNodeEntry = privateNodes.length > 0;
  const { isAuthenticated, setIsAuthenticated } = useAuth();
  const muiTheme = useMuiTheme();
  const colors = getThemeColors(muiTheme.palette.mode === 'dark');
  const navigate = useNavigate();
  const { t, i18n } = useTranslation();
  const { links } = useAppLinks();
  const [appVersion, setAppVersion] = useState<string>("");
  const [selectedLanguage, setSelectedLanguage] = useState<LanguageCode>(normalizeLanguageCode(i18n.language || ''));
  const [languageDialogOpen, setLanguageDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [showPasswordDialog, setShowPasswordDialog] = useState(false);
  const [loginOtherDialogOpen, setLoginOtherDialogOpen] = useState(false);
  const { showAlert } = useAlert();
  // 零身份设备账号（spec 2026-10-20 §4.4 / §4.6）：只存在于 Keychain 身份新壳，已登录但没有
  // 任何登录身份。没有普通退出（退出 = 账号丢失），邮箱 / 密码项换成可复制的用户 ID。
  const zeroIdentity =
    window._platform?.keychainIdentity === true &&
    isAuthenticated &&
    !!user &&
    !(user.loginIdentifies?.length > 0);
  // Keychain 新壳上 user 没加载出来（加载中，或 /api/user/info 失败）= 身份未知，不能当成有身份：
  // 对零身份账号点「切换账号」会作废设备账号的 token（spec §4.4）。身份确认之前不出退出按钮。
  const identityUnknown =
    window._platform?.keychainIdentity === true && isAuthenticated && !user;
  useEffect(() => {
    let version = window._platform!.version;
    // iOS App Store 审核要求：版本号不能包含 beta 后缀（如 0.4.0-beta.4），
    // 否则会被视为未完成的测试版本而被拒绝。仅影响显示，不影响实际版本逻辑。
    if (window._platform?.os === 'ios') {
      version = version.replace(/-beta.*$/, '');
    }
    setAppVersion(version);
  }, []);

  // iOS + Apple 订阅：退款时是否向 Apple 提供使用情况（null = 从未问过，按关闭显示）。
  const showAppleConsent =
    window._platform?.os === 'ios' && !!user?.subscriptions?.some((s) => s.provider === 'apple');
  const [consentOverride, setConsentOverride] = useState<boolean | null>(null);
  const [consentBusy, setConsentBusy] = useState(false);
  const appleConsentOn = consentOverride ?? user?.appleConsumptionConsent === true;
  const handleAppleConsentToggle = async () => {
    if (consentBusy) return;
    const next = !appleConsentOn;
    setConsentBusy(true);
    setConsentOverride(next);
    const ok = await setAppleConsumptionConsent(next);
    if (!ok) setConsentOverride(null);
    setConsentBusy(false);
  };

  const handleLogout = async () => {
    try {
      // Stop VPN first, then logout (tokens cleared automatically by cloudApi)
      await window._k2.run('down');
      await cloudApi.post('/api/auth/logout');
      setIsAuthenticated(false);
      console.info(t('common:messages.logoutSuccess'));
    } catch (err) {
      console.error(t('common:messages.logoutFailed'), err);
    }
  };

  const openLoginDialog = () => {
    useLoginDialogStore.getState().open({ trigger: 'account:login-other' });
  };

  // 「登录其他账号」：设备账号有会员时先提醒设置邮箱（spec §4.4），否则登录后找不回。
  const handleLoginOther = () => {
    if (isMembership) {
      setLoginOtherDialogOpen(true);
      return;
    }
    openLoginDialog();
  };

  const handleCopyUserId = async () => {
    if (!user?.uuid) return;
    try {
      await window._platform?.writeClipboard?.(user.uuid);
      showAlert(t('common:messages.copySuccess'), "success");
    } catch {
      showAlert(t('common:messages.copyFailed'), "error");
    }
  };

  const handleDeleteAccount = async () => {
    setDeleteLoading(true);
    setDeleteError(null);
    try {
      await cloudApi.request('DELETE', '/api/user/delete-account');
      setDeleteDialogOpen(false);
      // Same logout logic as handleLogout
      await window._k2.run('down');
      await cloudApi.post('/api/auth/logout');
      setIsAuthenticated(false);
    } catch (err) {
      console.error('Delete account failed:', err);
      // Rendered inside the dialog, which is still open on this path. It used
      // to be window.alert, which the Capacitor WebView swallows without a
      // sound — on mobile a failed deletion showed the user nothing at all,
      // on the one flow both app stores make a reviewer walk through.
      setDeleteError(t('account:account.deleteAccountFailed'));
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleLanguageChange = async (langCode: LanguageCode) => {
    setLanguageDialogOpen(false);
    if (langCode === selectedLanguage) return;
    setSelectedLanguage(langCode);

    // Use changeLanguage from i18n.ts which handles lazy loading of language resources
    await changeLanguage(langCode);

    // Sync locale to native layer (update Tray menu etc.)
    // This is a best-effort call - failures don't affect the UI language change
    window._platform!.syncLocale?.(langCode).catch((error: unknown) => {
      console.warn('Failed to sync locale to native layer:', error);
    });

    // If user is authenticated, update language preference on server
    if (isAuthenticated) {
      try {
        await cloudApi.request<DataUser>('PUT', '/api/user/language', { language: langCode });
        console.info('Language preference updated on server');
      } catch (error) {
        console.error('Failed to update language preference on server:', error);
        // Continue with local language switch even if server update fails
      }
    }
  };

  // 区分未登录和请求失败
  const isNotLoggedIn = !isAuthenticated;
  const hasError = !loading && !user && isAuthenticated; // 已登录但请求失败

  // Display time formatting (matching AppBarMembership)
  const displayTime = user?.expiredAt ? formatDate(user.expiredAt) : "-";
  // 从未开通（如免登录新建的设备账号）：不是「已过期」，别用错误色吓人，
  // 也没有过期日期可显示
  const neverSubscribed = !!user && !user.expiredAt;

  // Get status label and colors
  const getStatusLabel = () => {
    if (isNotLoggedIn) return t('account:account.notLoggedIn', '未登录');
    if (hasError) return t('common:common.loadFailed');
    if (neverSubscribed) return t('account:account.notSubscribed');
    if (isExpired) return t('nav:appBarMembership.expired');
    return t('nav:appBarMembership.standard');
  };

  const getStatusColor = () => {
    if (isNotLoggedIn) return 'info';
    if (hasError) return 'warning';
    if (neverSubscribed) return 'info';
    if (isExpired) return 'error';
    return 'success';
  };

  const getStatusIcon = () => {
    if (isNotLoggedIn) return <MembershipIcon sx={{ fontSize: 20 }} color="info" />;
    if (hasError) return <ErrorIcon sx={{ fontSize: 20 }} />;
    if (neverSubscribed) return <MembershipIcon sx={{ fontSize: 20 }} color="info" />;
    if (isExpired) return <ErrorIcon sx={{ fontSize: 20 }} />;
    return <CheckCircleIcon sx={{ fontSize: 20 }} />;
  };

  // Get membership card background gradient
  const getMembershipCardBackground = () => {
    if (loading) return muiTheme.palette.background.paper;
    if (isNotLoggedIn || neverSubscribed) {
      return `linear-gradient(135deg, ${colors.infoBgLight} 0%, ${colors.infoBgLighter} 100%)`;
    }
    if (hasError) {
      return `linear-gradient(135deg, ${colors.warningBgLight} 0%, ${colors.warningBgLighter} 100%)`;
    }
    if (isExpired) {
      return `linear-gradient(135deg, ${colors.errorBgLight} 0%, ${colors.errorBgLighter} 100%)`;
    }
    return `linear-gradient(135deg, ${colors.successBgLight} 0%, ${colors.successBgLighter} 100%)`;
  };

  // Get membership card border
  const getMembershipCardBorder = () => {
    if (loading) return colors.divider;
    if (isNotLoggedIn) return colors.infoBorder;
    if (hasError) return colors.warningBorder;
    if (neverSubscribed) return colors.infoBorder;
    if (isExpired) return colors.errorBorder;
    return colors.successBorder;
  };

  return (
    <Box sx={{
      width: "100%",
      py: 0.5,
      backgroundColor: "transparent"
    }}>
      {/* Brand Banner — on iOS the external kaitu.io link is removed (Apple 3.1.1:
          the app must not route to an external site that sells membership). The
          banner stays as a static brand header; other platforms keep the link. */}
      <Card
        sx={{
          mb: 2,
          background: (theme) => brandConfig.theme.surface.banner[theme.palette.mode],
          border: 'none',
          ...(window._platform?.os !== 'ios' && {
            cursor: 'pointer',
            transition: 'all 0.3s ease',
            '&:hover': {
              transform: 'translateY(-2px)',
              boxShadow: 3,
            },
          }),
        }}
        onClick={
          window._platform?.os === 'ios'
            ? undefined
            : () => window._platform!.openExternal?.(brandConfig.baseURL)
        }
      >
        <CardContent sx={{ py: 2, px: 2.5, '&:last-child': { pb: 2 }, textAlign: 'center' }}>
          <Typography
            variant="h5"
            sx={{
              fontWeight: 700,
              color: 'white',
              letterSpacing: '0.5px',
              textShadow: '0 2px 4px rgba(0,0,0,0.2)',
            }}
          >
            {brandConfig.domainLabel}{getBrandName(i18n.language) !== brandConfig.productName ? ` ${getBrandName(i18n.language)}` : ''}
          </Typography>
          <Typography
            variant="caption"
            sx={{
              color: 'rgba(255, 255, 255, 0.85)',
              fontSize: '0.7rem',
              mt: 0.5,
              display: 'block'
            }}
            dir="auto"
          >
            {getBrandSlogan(i18n.language)}
          </Typography>
        </CardContent>
      </Card>

      {/* Membership Status Card */}
      <Card
        sx={{
          mb: 2,
          background: getMembershipCardBackground(),
          border: `1px solid ${getMembershipCardBorder()}`,
        }}
      >
        <CardContent sx={{ py: 1.5, px: 2, '&:last-child': { pb: 1.5 } }}>
          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', py: 2 }}>
              <CircularProgress size={28} />
            </Box>
          ) : (
            <Stack spacing={1}>
              {/* Header with title and refresh button */}
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                  <MembershipIcon color={getStatusColor()} sx={{ fontSize: 20 }} />
                  <Typography variant="subtitle1" fontWeight={600} sx={{ fontSize: '0.95rem' }}>
                    {t('account:account.membershipStatus')}
                  </Typography>
                </Box>
                <Button
                  size="small"
                  onClick={() => fetchUser(true)}
                  disabled={loading}
                  sx={{
                    minWidth: 'auto',
                    px: 0.75,
                    py: 0.5,
                    '& .MuiButton-startIcon': { margin: 0 }
                  }}
                >
                  <RefreshIcon sx={{ fontSize: 18 }} />
                </Button>
              </Box>

              {/* Status row with icon, chip, and date */}
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                {getStatusIcon()}
                <Chip
                  label={getStatusLabel()}
                  color={getStatusColor()}
                  size="small"
                  sx={{ fontWeight: 600, fontSize: '0.7rem', height: 22 }}
                />
                {!neverSubscribed && (
                <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 0.5 }}>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{
                      fontSize: '0.75rem',
                      whiteSpace: 'nowrap'
                    }}
                  >
                    {t('account:account.expiryDate')}:
                  </Typography>
                  <Typography
                    variant="body2"
                    fontWeight={600}
                    sx={{
                      fontSize: '0.85rem',
                      whiteSpace: 'nowrap'
                    }}
                  >
                    {displayTime}
                  </Typography>
                </Box>
                )}
                {/* Quota info */}
                {user?.tier && (
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5, flexWrap: 'wrap' }}>
                    <Typography variant="caption" color="text.secondary">
                      {t('account:account.quotaDevices', { count: user.maxDevice || 5 })}
                      {(user.maxRouterDevice ?? 0) > 0 && ` · ${t('account:account.quotaRouter')}`}
                    </Typography>
                  </Box>
                )}
              </Box>

              {/* Action button */}
              {isNotLoggedIn && (
                <Button
                  variant="contained"
                  color="primary"
                  size="small"
                  fullWidth
                  onClick={() => {
                    // 打开登录弹窗
                    const { open } = useLoginDialogStore.getState();
                    open({
                      trigger: 'account:membership-card',
                      message: t('account:account.loginToViewMembership', '登录后查看会员状态'),
                    });
                  }}
                  sx={{
                    borderRadius: 1.5,
                    textTransform: 'none',
                    fontWeight: 600,
                    fontSize: '0.85rem',
                    py: 0.75,
                    mt: 0.5
                  }}
                >
                  {t('auth:auth.login', '登录')}
                </Button>
              )}
              {hasError && (
                <Button
                  variant="contained"
                  color="warning"
                  size="small"
                  fullWidth
                  startIcon={<RefreshIcon sx={{ fontSize: 18 }} />}
                  onClick={() => fetchUser(true)}
                  disabled={loading}
                  sx={{
                    borderRadius: 1.5,
                    textTransform: 'none',
                    fontWeight: 600,
                    fontSize: '0.85rem',
                    py: 0.75,
                    mt: 0.5
                  }}
                >
                  {t('common:common.retry')}
                </Button>
              )}
              {/* Purchase CTAs: purchaseSurfaceAvailable() (utils/purchase-surface.ts)
                  is the single gate; the iOS affordance clause stays as before. */}
              {!isNotLoggedIn && !hasError && isExpired && purchaseSurfaceAvailable() &&
  (window._platform?.os !== 'ios' || affordance.mode === 'subscribe') && (
                <Button
                  variant="contained"
                  color="primary"
                  size="small"
                  fullWidth
                  startIcon={<ShoppingCartIcon sx={{ fontSize: 18 }} />}
                  onClick={() => navigate("/purchase", { state: { from: 'account_expired' } })}
                  sx={{
                    borderRadius: 1.5,
                    textTransform: 'none',
                    fontWeight: 600,
                    fontSize: '0.85rem',
                    py: 0.75,
                    mt: 0.5
                  }}
                >
                  {t(neverSubscribed ? 'account:account.subscribeNow' : 'account:account.renewNow')}
                </Button>
              )}
              {!isNotLoggedIn && !hasError && !purchaseSurfaceAvailable() && window._platform?.os === 'android' && (
                <SubscriptionStatusOnly isPro={!isExpired} expiresText={displayTime} />
              )}
            </Stack>
          )}
        </CardContent>
      </Card>

      {/* 专属节点入口（轻量）：仅已有线路时显示，详情在管理页 */}
      {showPrivateNodeEntry && (
        <Card sx={{ mb: 2 }}>
          <List disablePadding>
            <ListItem
              button
              onClick={() => navigate("/private-node")}
              sx={{
                py: 1.5,
                cursor: 'pointer',
                '&:hover': { backgroundColor: 'action.hover' },
              }}
            >
              <ListItemIcon>
                <DnsIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography component="span" variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                      {t('privateNode:privateNode.myRouter')}
                    </Typography>
                    {privateNodes.length > 0 && (
                      <Chip
                        label={privateNodes.length}
                        color="primary"
                        size="small"
                        sx={{ height: 20, fontSize: '0.7rem', fontWeight: 600 }}
                      />
                    )}
                  </Box>
                }
              />
              <ListItemSecondaryAction>
                <ChevronRightIcon color="action" />
              </ListItemSecondaryAction>
            </ListItem>
          </List>
        </Card>
      )}

      <Box
        sx={(theme) => ({
          borderRadius: 2,
          background: theme.palette.mode === 'dark'
            ? `linear-gradient(145deg, ${theme.palette.grey[800]} 0%, ${theme.palette.grey[900]} 100%)`
            : `linear-gradient(145deg, ${theme.palette.background.paper} 0%, ${theme.palette.grey[50]} 100%)`,
          boxShadow: 'none',
          border: theme.palette.mode === 'dark'
            ? `1px solid ${theme.palette.grey[700]}`
            : `1px solid ${theme.palette.grey[200]}`,
        })}
      >
          <List>
            {zeroIdentity ? (
            <ListItem sx={{ py: 1.5 }} data-testid="account-user-id">
              <ListItemIcon>
                <UserIdIcon />
              </ListItemIcon>
              <ListItemText
                sx={{ minWidth: 0 }}
                primary={
                  <Typography variant="body2" component="span" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.userId')}
                  </Typography>
                }
                secondary={
                  <Typography
                    variant="caption"
                    component="span"
                    color="text.secondary"
                    sx={{ fontFamily: 'monospace', overflowWrap: 'anywhere', userSelect: 'text' }}
                  >
                    {user!.uuid}
                  </Typography>
                }
              />
              <IconButton
                size="small"
                onClick={handleCopyUserId}
                aria-label={t('account:account.copyUserId')}
                sx={{ ml: 1, flexShrink: 0 }}
              >
                <CopyIcon fontSize="small" />
              </IconButton>
            </ListItem>
            ) : (<>
            <ListItem
              disabled={!isMembership}
              sx={{
                py: 1.5,
                '&:hover': !isMembership ? {} : {
                  backgroundColor: 'action.hover',
                }
              }}
            >
              <ListItemIcon>
                <EmailIcon />
              </ListItemIcon>
              <ListItemText
                sx={{ minWidth: 0 }}
                primary={
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1, minWidth: 0 }}>
                    <Typography
                      variant="body2"
                      component="span"
                      sx={{
                        fontWeight: 500,
                        fontSize: '0.9rem',
                        minWidth: 0,
                        // 邮箱是用户可控的无界内容，且掩码星号串没有天然断行点
                        overflowWrap: 'anywhere',
                      }}
                    >
                      {user?.loginIdentifies?.find(i => i.type === 'email')?.value ? maskEmail(user.loginIdentifies.find(i => i.type === 'email')!.value) : t('account:account.loginEmail')}
                    </Typography>
                    {!isMembership && (
                      <Chip
                        label={t('account:account.proOnly')}
                        color="warning"
                        size="small"
                        variant="outlined"
                        sx={{
                          ml: 0,
                          flexShrink: 0,
                          fontWeight: 500,
                          fontSize: '0.75rem',
                          height: '20px'
                        }}
                      />
                    )}
                  </Box>
                }
              />
              {/* 次要动作留在正常流内（而非 ListItemSecondaryAction 的绝对定位）：
                  这里是文字按钮，宽度远超 MUI 为图标预留的 48px paddingRight，
                  绝对定位会让邮箱文字压到按钮底下。 */}
              <Box sx={{ flexShrink: 0, ml: 1 }}>
                {isMembership ? (
                  !!!user?.loginIdentifies?.find(i => i.type === 'email')?.value ? (
                    <Button
                      size="small"
                      variant="contained"
                      color="warning"
                      onClick={() => navigate("/update-email")}
                      sx={{
                        borderRadius: 1.5,
                        textTransform: 'none',
                        fontWeight: 500,
                        fontSize: '0.8rem',
                        px: 2,
                        py: 0.5
                      }}
                    >
                      {t('account:account.setEmail')}
                    </Button>
                  ) : (
                    <Button
                      size="small"
                      variant="outlined"
                      onClick={() => navigate("/update-email")}
                      sx={{
                        borderRadius: 1.5,
                        textTransform: 'none',
                        fontWeight: 500,
                        fontSize: '0.8rem',
                        px: 2,
                        py: 0.5
                      }}
                    >
                      {t('account:account.modifyEmail')}
                    </Button>
                  )
                ) : (purchaseSurfaceAvailable() && (window._platform?.os !== 'ios' || affordance.mode === 'subscribe')) ? (
                  <Button
                    size="small"
                    variant="contained"
                    color="error"
                    onClick={() => navigate("/purchase", { state: { from: 'account' } })}
                    sx={{
                      borderRadius: 1.5,
                      textTransform: 'none',
                      fontWeight: 500,
                      fontSize: '0.8rem',
                      px: 2,
                      py: 0.5
                    }}
                  >
                    {t('account:account.proPlan')}
                  </Button>
                ) : null}
              </Box>
            </ListItem>

            {/* 多设备权益但未设置邮箱的提醒 */}
            {isMembership && !!!user?.loginIdentifies?.find(i => i.type === 'email')?.value && (
              <Box sx={{ mx: 1.5, mb: 0.5 }}>
                <Alert
                  severity="warning"
                  variant="outlined"
                  sx={{
                    fontWeight: 500,
                    borderRadius: 1.5,
                  }}
                >
                  {t('account:account.multiDeviceWarning')}
                </Alert>
              </Box>
            )}

            <Divider />

            <ListItem
              button
              onClick={() => setShowPasswordDialog(true)}
              disabled={!isAuthenticated}
              sx={{
                py: 1.5,
                cursor: 'pointer',
                '&:hover': {
                  backgroundColor: 'action.hover',
                }
              }}
            >
              <ListItemIcon>
                <LockIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {user?.hasPassword
                      ? t('account:password.changePassword')
                      : t('account:password.setPassword')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <ChevronRightIcon color="action" />
              </ListItemSecondaryAction>
            </ListItem>
            </>)}

            <Divider />

            <ListItem
              button={true}
              onClick={() => navigate("/devices")}
              sx={{
                py: 1.5,
                cursor: 'pointer',
                '&:hover': {
                  backgroundColor: 'action.hover',
                }
              }}
            >
              <ListItemIcon>
                <DevicesIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography component="span" variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.myDevices')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <ChevronRightIcon color="action" />
              </ListItemSecondaryAction>
            </ListItem>

            {/* 代付人设置（第三方代付）— 品牌门 features.delegate（开途专属）；iOS 隐藏：Apple 3.1.1 禁止 IAP 以外的支付路径 */}
            {getCurrentAppConfig().features.delegate && window._platform?.os !== 'ios' && (<>
            <Divider />

            <ListItem
              sx={{
                cursor: 'pointer',
                py: 1.5,
                '&:hover': {
                  backgroundColor: 'action.hover'
                }
              }}
              onClick={() => navigate("/delegate")}
            >
              <ListItemIcon>
                <GroupIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography component="span" variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.delegate')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <ChevronRightIcon color="action" />
              </ListItemSecondaryAction>
            </ListItem>
            </>)}

            <Divider />

            <ListItem
              sx={{
                cursor: 'pointer',
                py: 1.5,
                '&:hover': {
                  backgroundColor: 'action.hover'
                }
              }}
              onClick={() => navigate("/pro-histories?from=/account")}
              secondaryAction={<ChevronRightIcon color="action" />}
            >
              <ListItemIcon>
                <HistoryIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.paymentHistory')}
                  </Typography>
                }
              />
            </ListItem>

            {/* 我的钱包（外部钱包/储值）— 品牌门 features.wallet（开途专属，WordGate 钱包）；iOS 隐藏：Apple 3.1.1 禁止 IAP 以外的支付/充值路径 */}
            {getCurrentAppConfig().features.wallet && window._platform?.os !== 'ios' && (<>
            <Divider />

            {/* Wallet */}
            <ListItem
              sx={{
                cursor: 'pointer',
                py: 1.5,
                '&:hover': {
                  backgroundColor: 'action.hover'
                }
              }}
              onClick={() => window._platform!.openExternal?.(links.walletUrl)}
              secondaryAction={<ChevronRightIcon color="action" />}
            >
              <ListItemIcon>
                <WalletIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.wallet')}
                  </Typography>
                }
              />
            </ListItem>
            </>)}

            <Divider />

            {/* Device Install Guide */}
            <ListItem
              sx={{
                cursor: 'pointer',
                py: 1.5,
                '&:hover': {
                  backgroundColor: 'action.hover'
                }
              }}
              onClick={() => navigate("/device-install")}
              secondaryAction={<ChevronRightIcon color="action" />}
            >
              <ListItemIcon>
                <PhoneAndroidIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('nav:layout.deviceInstall')}
                  </Typography>
                }
              />
            </ListItem>

            <Divider />

            {/* Feedback */}
            <ListItem
              sx={{
                cursor: 'pointer',
                py: 1.5,
                '&:hover': {
                  backgroundColor: 'action.hover'
                }
              }}
              onClick={() => navigate("/feedback")}
              secondaryAction={<ChevronRightIcon color="action" />}
            >
              <ListItemIcon>
                <FeedbackIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('nav:navigation.faq')}
                  </Typography>
                }
              />
            </ListItem>

            <Divider />

            {showAppleConsent && (<>
            <ListItem sx={{ py: 1.5 }} data-testid="account-apple-consent">
              <ListItemIcon>
                <PrivacyTipIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography component="span" variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('purchase:purchase.iap.consentSetting')}
                  </Typography>
                }
              />
              <Switch
                edge="end"
                checked={appleConsentOn}
                disabled={consentBusy}
                onChange={() => void handleAppleConsentToggle()}
                inputProps={{ 'aria-label': t('purchase:purchase.iap.consentSetting') }}
              />
            </ListItem>
            <Divider />
            </>)}

            <ListItem sx={{ py: 1.5 }}>
              <ListItemIcon>
                <LanguageIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('account:account.language')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <Button
                  variant="outlined"
                  size="small"
                  onClick={() => setLanguageDialogOpen(true)}
                  endIcon={<ExpandMoreIcon />}
                  aria-label={t('account:account.selectLanguage')}
                  sx={{ borderRadius: 1.5, textTransform: 'none', fontSize: '0.8rem', minWidth: 140, justifyContent: 'space-between' }}
                >
                  {languages[selectedLanguage].nativeName}
                </Button>
              </ListItemSecondaryAction>
            </ListItem>

            <Divider />

            <BetaChannelToggle />

            <VersionItem appVersion={appVersion} commit={window._platform?.commit} />
          </List>
      </Box>

      {/* 零身份：没有普通退出，换成【其他设备安装】和「登录其他账号」（spec §4.4） */}
      {zeroIdentity && (
        <Box sx={{ mt: 1, display: "flex", justifyContent: "center", gap: 1.5, flexWrap: 'wrap' }}>
          <Button
            variant="contained"
            data-testid="zero-identity-device-install"
            startIcon={<PhoneAndroidIcon />}
            onClick={() => navigate("/device-install")}
            sx={{ px: 2.5, py: 0.75, borderRadius: 2, textTransform: 'none', fontWeight: 500, fontSize: '0.85rem' }}
          >
            {t('nav:layout.deviceInstall')}
          </Button>
          <Button
            variant="outlined"
            onClick={handleLoginOther}
            sx={{ px: 2.5, py: 0.75, borderRadius: 2, textTransform: 'none', fontWeight: 500, fontSize: '0.85rem' }}
          >
            {t('account:account.loginOtherAccount')}
          </Button>
        </Box>
      )}

      <Dialog open={loginOtherDialogOpen} onClose={() => setLoginOtherDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogContent>
          <DialogContentText>{t('account:account.loginOtherWarning')}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => {
              setLoginOtherDialogOpen(false);
              openLoginDialog();
            }}
          >
            {t('account:account.loginAnyway')}
          </Button>
          <Button
            variant="contained"
            onClick={() => {
              setLoginOtherDialogOpen(false);
              navigate("/device-install");
            }}
          >
            {t('account:account.goSetEmail')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 切换账号按钮 - 仅登录状态显示 */}
      {isAuthenticated && !zeroIdentity && !identityUnknown && (
        <Box sx={{ mt: 1, display: "flex", justifyContent: "center", gap: 1.5 }}>
          <Button
            variant="outlined"
            color="error"
            startIcon={<LogoutIcon />}
            onClick={handleLogout}
            sx={{
              px: 2.5,
              py: 0.75,
              borderRadius: 2,
              textTransform: 'none',
              fontWeight: 500,
              fontSize: '0.85rem',
            }}
          >
            {t('account:account.switchAccount')}
          </Button>
        </Box>
      )}

      {/* 注销账号按钮 - 仅登录状态显示 */}
      {isAuthenticated && (
        <Box sx={{ mt: 1, display: "flex", justifyContent: "center" }}>
          <Button
            color="inherit"
            onClick={() => setDeleteDialogOpen(true)}
            sx={{
              color: 'text.disabled',
              textTransform: 'none',
              fontSize: '0.8rem',
            }}
          >
            {t('account:account.deleteAccount')}
          </Button>
        </Box>
      )}

      {/* 注销账号确认对话框：三步确认 + 资产损失高亮，见 DeleteAccountDialog */}
      <DeleteAccountDialog
        open={deleteDialogOpen}
        user={user}
        loading={deleteLoading}
        error={deleteError}
        onClose={() => {
          setDeleteDialogOpen(false);
          setDeleteError(null);
        }}
        onConfirm={handleDeleteAccount}
      />

      <LanguageDialog
        open={languageDialogOpen}
        value={selectedLanguage}
        onClose={() => setLanguageDialogOpen(false)}
        onSelect={handleLanguageChange}
      />

      <PasswordDialog
        open={showPasswordDialog}
        hasPassword={user?.hasPassword ?? false}
        userEmail={user?.loginIdentifies?.find((i) => i.type === 'email')?.value ?? ''}
        onClose={() => setShowPasswordDialog(false)}
        onSuccess={() => {
          setShowPasswordDialog(false);
          fetchUser();
        }}
      />
    </Box>
  );
}
