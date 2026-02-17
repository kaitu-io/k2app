/**
 * CompactConnectionButton - iOS 风格紧凑连接按钮
 *
 * 折叠状态下显示的紧凑视图：
 * - 左侧：服务器图标和名称
 * - 中间：连接状态
 * - 右侧：Switch 开关
 */

import { useMemo } from 'react';
import {
  Box,
  Typography,
  Switch,
  CircularProgress,
  styled,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { getThemeColors, getStatusColor } from '../theme/colors';
import { getFlagIcon } from '../utils/country';

type ServiceState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnecting'
  | 'error';

// iOS 风格的 List Item 容器
const CompactContainer = styled(Box, {
  shouldForwardProp: (prop) => prop !== 'flush',
})<{ flush?: boolean }>(({ theme, flush }) => {
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

  return {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: flush ? '10px 12px' : '12px 16px',
    borderRadius: flush ? 0 : 12,
    backgroundColor: isDark ? colors.cardBg : '#ffffff',
    border: flush ? 'none' : `1px solid ${isDark ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.08)'}`,
    borderBottom: flush ? `1px solid ${isDark ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.08)'}` : undefined,
    transition: 'all 0.2s ease',
    minHeight: flush ? 48 : 56,
    width: '100%',
    boxSizing: 'border-box',
    '&:hover': {
      backgroundColor: isDark ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.02)',
    },
  };
});

// 将 ServiceState 映射到视觉状态
// isRetrying: error 状态下 K2 是否在重试，true 则显示为 transitioning
function mapServiceStateToVisual(status: ServiceState, isRetrying = false): 'connected' | 'transitioning' | 'disconnected' | 'disabled' {
  switch (status) {
    case 'connected':
      return 'connected';
    case 'connecting':
    case 'reconnecting':
    case 'disconnecting':
      return 'transitioning';
    case 'error':
      // error + retrying 视觉上显示为 transitioning（脉冲效果）
      return isRetrying ? 'transitioning' : 'disabled';
    case 'disconnected':
    default:
      return 'disconnected';
  }
}

// 状态指示器圆点
const StatusDot = styled(Box, {
  shouldForwardProp: (prop) => prop !== 'visualStatus' && prop !== 'isTransitioning',
})<{ visualStatus: 'connected' | 'transitioning' | 'disconnected' | 'disabled'; isTransitioning: boolean }>(({ theme, visualStatus, isTransitioning }) => {
  const isDark = theme.palette.mode === 'dark';
  const color = getStatusColor(visualStatus, isDark);

  return {
    width: 8,
    height: 8,
    borderRadius: '50%',
    backgroundColor: color,
    marginRight: 6,
    flexShrink: 0,
    animation: isTransitioning ? 'pulse 1.5s infinite' : 'none',
    '@keyframes pulse': {
      '0%, 100%': { opacity: 1 },
      '50%': { opacity: 0.4 },
    },
  };
});

export interface CompactConnectionButtonProps {
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
  /** Flush display (no rounded corners, no margin) */
  flush?: boolean;
  /** Error state: whether K2 is retrying */
  isRetrying?: boolean;
  /** Whether network is available during error retry */
  networkAvailable?: boolean;
}

export function CompactConnectionButton({
  serviceState,
  hasTunnelSelected,
  tunnelName,
  tunnelCountry,
  onToggle,
  flush = false,
  isRetrying = false,
  networkAvailable = true,
}: CompactConnectionButtonProps) {
  const { t } = useTranslation();

  const isConnected = serviceState === 'connected';
  const isConnecting = serviceState === 'connecting';
  const isDisconnected = serviceState === 'disconnected';
  const isDisconnecting = serviceState === 'disconnecting';
  const isReconnecting = serviceState === 'reconnecting';
  const isError = serviceState === 'error';

  // error + retrying 视觉上等同于 reconnecting
  const isErrorRetrying = isError && isRetrying;
  const isTransitioning = isConnecting || isReconnecting || isDisconnecting || isErrorRetrying;

  // Switch 状态：已连接或正在连接/重试时为开启
  const switchChecked = isConnected || isConnecting || isReconnecting || isErrorRetrying;

  // Switch 禁用：断开中或未选择服务器
  const switchDisabled = isDisconnecting || (isDisconnected && !hasTunnelSelected);

  // Status text
  const statusText = useMemo(() => {
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
  }, [serviceState, isRetrying, networkAvailable, t]);

  // Server display name
  const serverName = useMemo(() => {
    if (!hasTunnelSelected) {
      return t('dashboard:dashboard.selectServerHint') || 'Select a server';
    }
    return tunnelName || t('dashboard:dashboard.selectedServer') || 'Selected Server';
  }, [hasTunnelSelected, tunnelName, t]);

  return (
    <CompactContainer flush={flush}>
      {/* Left: Server info */}
      <Box sx={{ display: 'flex', alignItems: 'center', flex: 1, minWidth: 0 }}>
        {tunnelCountry ? (
          <Box
            sx={{
              mr: 1.5,
              flexShrink: 0,
              display: 'flex',
              alignItems: 'center',
              '& svg': { width: 24, height: 16, borderRadius: 0.5 }
            }}
          >
            {getFlagIcon(tunnelCountry)}
          </Box>
        ) : (
          <Box
            sx={{
              width: 24,
              height: 24,
              mr: 1.5,
              flexShrink: 0,
              borderRadius: '50%',
              backgroundColor: hasTunnelSelected ? 'primary.main' : 'action.disabled',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <Typography sx={{ fontSize: 14, color: hasTunnelSelected ? 'primary.contrastText' : 'text.disabled' }}>
              {hasTunnelSelected ? '🌐' : '?'}
            </Typography>
          </Box>
        )}
        <Typography
          variant="body1"
          fontWeight={600}
          sx={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            color: hasTunnelSelected ? 'text.primary' : 'text.secondary',
          }}
        >
          {serverName}
        </Typography>
      </Box>

      {/* 中间：状态显示 */}
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          mx: 2,
          flexShrink: 0,
        }}
      >
        {isTransitioning ? (
          <CircularProgress size={12} sx={{ mr: 1 }} />
        ) : (
          <StatusDot visualStatus={mapServiceStateToVisual(serviceState, isRetrying)} isTransitioning={isTransitioning} />
        )}
        <Typography
          variant="caption"
          sx={{
            fontWeight: 500,
            color: 'text.secondary',
            fontSize: '0.75rem',
          }}
        >
          {statusText}
        </Typography>
      </Box>

      {/* 右侧：Switch 开关 */}
      <Switch
        checked={switchChecked}
        onChange={onToggle}
        disabled={switchDisabled}
        color="success"
        sx={{
          '& .MuiSwitch-switchBase.Mui-checked': {
            color: '#4caf50',
          },
          '& .MuiSwitch-switchBase.Mui-checked + .MuiSwitch-track': {
            backgroundColor: '#4caf50',
          },
        }}
      />
    </CompactContainer>
  );
}

export default CompactConnectionButton;