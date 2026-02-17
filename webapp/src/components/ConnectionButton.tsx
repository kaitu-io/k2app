/**
 * ConnectionButton - VPN 连接大按钮组件
 *
 * 特性：
 * - 220px 圆形大按钮 (ExpressVPN 风格)
 * - 状态驱动的颜色/动画
 * - hover 时在 connecting/connected 状态显示停止提示
 * - 允许在 connecting 状态点击取消连接
 */

import { useState, useMemo } from 'react';
import {
  Box,
  Typography,
  Stack,
  Tooltip,
  CircularProgress,
  Button,
  styled,
} from '@mui/material';
import { PlayArrow, Stop } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { getThemeColors, getStatusGradient, getStatusShadow } from '../theme/colors';
import { getFlagIcon } from '../utils/country';

type ServiceState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnecting'
  | 'error';

// 按钮视觉状态
type VisualStatus = 'connected' | 'transitioning' | 'disconnected' | 'stop';

// Styled Component - 从 Dashboard.tsx 迁移并增强
const StyledConnectionButton = styled(Button, {
  shouldForwardProp: (prop) =>
    !['visualStatus', 'buttonSize'].includes(prop as string)
})<{
  visualStatus: VisualStatus;
  buttonSize: number;
}>(({ theme, visualStatus, buttonSize }) => {
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

  return {
    width: buttonSize,
    height: buttonSize,
    borderRadius: '50%',
    position: 'relative',
    overflow: 'hidden',
    border: 'none',
    transition: 'all 0.5s cubic-bezier(0.4, 0, 0.2, 1)',
    background: getStatusGradient(visualStatus, isDark),
    boxShadow: getStatusShadow(visualStatus, isDark),
    animation: visualStatus === 'transitioning'
      ? 'pulse 2s infinite'
      : visualStatus === 'disconnected'
        ? 'breathe 3s infinite'
        : 'none',
    '&::before': {
      content: '""',
      position: 'absolute',
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      background: colors.overlay,
      opacity: 0,
      transition: 'opacity 0.3s ease',
    },
    '&:hover': {
      transform: 'scale(1.05)',
      boxShadow: visualStatus === 'transitioning'
        ? `0 25px 80px ${colors.warningGlowStrong}`
        : visualStatus === 'connected'
          ? `0 25px 80px ${colors.successGlowStrong}`
          : visualStatus === 'stop'
            ? `0 25px 80px ${colors.errorGlowStrong}`
            : `0 25px 80px ${colors.infoGlowStrong}`,
      '&::before': {
        opacity: 1,
      },
    },
    '&:active': {
      transform: 'scale(0.98)',
    },
    '&:disabled': {
      background: colors.disabledGradient,
      boxShadow: colors.disabledShadow,
      transform: 'none',
    },
    '@keyframes pulse': {
      '0%, 100%': {
        boxShadow: getStatusShadow('transitioning', isDark),
      },
      '50%': {
        boxShadow: `0 20px 60px ${colors.warningGlowStrong}, 0 0 0 30px transparent`,
      },
    },
    '@keyframes breathe': {
      '0%, 100%': {
        boxShadow: getStatusShadow('disconnected', isDark),
      },
      '50%': {
        boxShadow: `0 20px 60px ${colors.infoGlowStrong}, 0 0 0 20px transparent`,
      },
    },
  };
});

export interface ConnectionButtonProps {
  /** VPN service state */
  serviceState: ServiceState;
  /** Whether a tunnel is selected (for disabled logic) */
  hasTunnelSelected: boolean;
  /** Display name for the selected tunnel (optional) */
  tunnelName?: string;
  /** ISO 3166-1 alpha-2 country code for flag display (optional) */
  tunnelCountry?: string;
  /** Toggle callback */
  onToggle: () => void;
  /** Button size (diameter), default 220 */
  size?: number;
  /** Error state: whether K2 is retrying (true=retrying, show animation; false=requires user action) */
  isRetrying?: boolean;
  /** Whether network is available during error retry (true=reconnecting to server, false=waiting for network) */
  networkAvailable?: boolean;
}

export function ConnectionButton({
  serviceState,
  hasTunnelSelected,
  tunnelName,
  tunnelCountry,
  onToggle,
  size = 220,
  isRetrying = false,
  networkAvailable = true,
}: ConnectionButtonProps) {
  const { t } = useTranslation();
  const [isHovered, setIsHovered] = useState(false);

  // 状态判断
  const isConnected = serviceState === 'connected';
  const isConnecting = serviceState === 'connecting';
  const isDisconnected = serviceState === 'disconnected';
  const isDisconnecting = serviceState === 'disconnecting';
  const isReconnecting = serviceState === 'reconnecting';
  const isError = serviceState === 'error';

  // error + retrying 视觉上等同于 reconnecting（脉冲动画）
  const isErrorRetrying = isError && isRetrying;
  const isTransitioning = isConnecting || isReconnecting || isDisconnecting || isErrorRetrying;

  // hover 时在可操作状态（connecting/connected/reconnecting/error+retrying）显示停止提示
  const showStopHint = isHovered && (isConnected || isConnecting || isReconnecting || isErrorRetrying);

  // 计算视觉状态
  const visualStatus: VisualStatus = useMemo(() => {
    if (showStopHint) {
      return 'stop';
    }
    if (isTransitioning) {
      return 'transitioning';
    }
    if (isConnected) {
      return 'connected';
    }
    return 'disconnected';
  }, [showStopHint, isTransitioning, isConnected]);

  // Status text
  const statusText = useMemo(() => {
    if (showStopHint) {
      return t('common:status.clickToStop');
    }

    switch (serviceState) {
      case 'connecting':
        return t('common:status.connecting');
      case 'connected':
        return t('common:status.connected');
      case 'reconnecting':
        return t('common:status.reconnecting');
      case 'disconnecting':
        return t('common:status.disconnecting');
      case 'error':
        // Show network-aware message when retrying
        if (isRetrying) {
          return networkAvailable
            ? t('common:status.reconnectingToServer')
            : t('common:status.waitingForNetwork');
        }
        return t('common:status.error');
      default:
        return t('common:status.disconnected');
    }
  }, [serviceState, showStopHint, isRetrying, networkAvailable, t]);

  // 按钮图标
  const ButtonIcon = useMemo(() => {
    // hover 时显示停止图标
    if (showStopHint) {
      return <Stop sx={{ fontSize: 50, color: 'white' }} />;
    }
    // 过渡状态（但不是 hover 停止状态）显示加载圈
    if (isTransitioning) {
      return <CircularProgress size={50} sx={{ color: 'white' }} />;
    }
    // 已连接显示停止图标
    if (isConnected) {
      return <Stop sx={{ fontSize: 50, color: 'white' }} />;
    }
    // 其他状态显示播放图标
    return <PlayArrow sx={{ fontSize: 50, color: 'white' }} />;
  }, [isTransitioning, isConnected, showStopHint]);

  // 按钮禁用逻辑
  // - 允许在 connecting/reconnecting 状态点击取消
  // - 禁用 disconnecting（等待操作完成）
  // - 禁用 disconnected 且未选择 tunnel
  const isButtonDisabled =
    isDisconnecting ||
    (isDisconnected && !hasTunnelSelected);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
      <Tooltip title={statusText} arrow placement="top">
        <span>
          <StyledConnectionButton
            visualStatus={visualStatus}
            buttonSize={size}
            onClick={onToggle}
            disabled={isButtonDisabled}
            onMouseEnter={() => setIsHovered(true)}
            onMouseLeave={() => setIsHovered(false)}
          >
            <Stack alignItems="center" justifyContent="center" spacing={1}>
              {ButtonIcon}
              <Typography
                variant="h6"
                fontWeight={700}
                color="white"
                sx={{ letterSpacing: 0.5 }}
              >
                {statusText}
              </Typography>
              {(tunnelName || tunnelCountry) && (
                <Box sx={{ mt: 0.5, display: 'flex', alignItems: 'center', gap: 0.5 }}>
                  {tunnelCountry && (
                    <Box sx={{
                      display: 'flex',
                      alignItems: 'center',
                      '& svg': { width: 18, height: 12, borderRadius: 0.5 }
                    }}>
                      {getFlagIcon(tunnelCountry)}
                    </Box>
                  )}
                  {tunnelName && (
                    <Typography
                      variant="caption"
                      sx={{ color: 'rgba(255,255,255,0.9)', fontSize: '0.7rem' }}
                    >
                      {tunnelName}
                    </Typography>
                  )}
                </Box>
              )}
            </Stack>
          </StyledConnectionButton>
        </span>
      </Tooltip>

      {/* 未选择服务器提示 */}
      {!hasTunnelSelected && isDisconnected && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ mt: 1.5, textAlign: 'center', maxWidth: 280, fontSize: '0.7rem' }}
        >
          {t('dashboard:dashboard.selectServerHint') || 'Select a server from the list below to get started'}
        </Typography>
      )}
    </Box>
  );
}

export default ConnectionButton;