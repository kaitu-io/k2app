/**
 * CollapsibleConnectionSection - 可折叠的 VPN 连接控制区域
 *
 * 特性：
 * - 展开状态：显示完整的 ConnectionButton (大圆形按钮)
 * - 折叠状态：显示紧凑的 CompactConnectionButton (iOS 风格 list item，贴边显示)
 * - 底部有折叠/展开切换图标
 * - 错误提示显示在折叠按钮下方
 * - 桌面版默认展开，路由器版默认折叠
 */

import { Box, IconButton, Collapse, Typography, Stack, styled, alpha } from '@mui/material';
import {
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Error as ErrorIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { ConnectionButton } from './ConnectionButton';
import { CompactConnectionButton } from './CompactConnectionButton';
import { useLayout } from '../stores/layout.store';
import type { ControlError } from '../services/vpn-types';
import { getErrorI18nKey } from '../services/vpn-types';

type ServiceState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnecting'
  | 'error';

// 折叠切换按钮容器 - 紧凑设计
const CollapseToggle = styled(Box)(({ theme }) => ({
  display: 'flex',
  justifyContent: 'center',
  alignItems: 'center',
  height: 20, // 固定高度，更紧凑
  cursor: 'pointer',
  transition: 'all 0.2s ease',
  '&:hover': {
    backgroundColor: theme.palette.action.hover,
  },
}));

// 切换按钮图标 - 更小的图标
const ToggleIconButton = styled(IconButton)(({ theme }) => ({
  padding: 0,
  height: 16,
  width: 32,
  '& .MuiSvgIcon-root': {
    fontSize: 16,
    color: theme.palette.text.disabled,
    transition: 'transform 0.3s ease',
  },
  '&:hover': {
    backgroundColor: 'transparent',
    '& .MuiSvgIcon-root': {
      color: theme.palette.text.secondary,
    },
  },
}));

// 内联错误提示容器
const InlineErrorBar = styled(Box)(({ theme }) => ({
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '6px 12px',
  backgroundColor: alpha(theme.palette.warning.main, theme.palette.mode === 'dark' ? 0.15 : 0.1),
  borderBottom: `1px solid ${alpha(theme.palette.warning.main, 0.3)}`,
  gap: 8,
}));

export interface CollapsibleConnectionSectionProps {
  /** VPN service state */
  serviceState: ServiceState;
  /** Whether a tunnel is selected (for disabled logic) */
  hasTunnelSelected: boolean;
  /** Display name for the selected tunnel */
  tunnelName?: string;
  /** ISO 3166-1 alpha-2 country code for flag display */
  tunnelCountry?: string;
  /** Toggle callback */
  onToggle: () => void;
  /** Connection error info */
  error?: ControlError | null;
  /** Error state: whether K2 is retrying */
  isRetrying?: boolean;
  /** Whether network is available during error retry */
  networkAvailable?: boolean;
}

export function CollapsibleConnectionSection({
  serviceState,
  hasTunnelSelected,
  tunnelName,
  tunnelCountry,
  onToggle,
  error,
  isRetrying = false,
  networkAvailable = true,
}: CollapsibleConnectionSectionProps) {
  const { t } = useTranslation();
  const { connectionButtonCollapsed, toggleConnectionButtonCollapsed } = useLayout();

  const showError = error && connectionButtonCollapsed;

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        // 展开时有底部边框，折叠时由 CompactConnectionButton 提供
        borderBottom: connectionButtonCollapsed ? 'none' : (theme) => `1px solid ${theme.palette.divider}`,
      }}
    >
      {/* Collapsed state: compact view (flush display) */}
      <Collapse in={connectionButtonCollapsed} timeout={300}>
        <CompactConnectionButton
          serviceState={serviceState}
          hasTunnelSelected={hasTunnelSelected}
          tunnelName={tunnelName}
          tunnelCountry={tunnelCountry}
          onToggle={onToggle}
          isRetrying={isRetrying}
          networkAvailable={networkAvailable}
          flush
        />
      </Collapse>

      {/* 折叠状态下的错误提示 */}
      <Collapse in={!!showError} timeout={200}>
        <InlineErrorBar>
          <Stack direction="row" spacing={1} alignItems="center" sx={{ flex: 1, minWidth: 0 }}>
            <ErrorIcon sx={{ fontSize: 16, color: 'error.main', flexShrink: 0 }} />
            <Typography
              variant="caption"
              sx={{
                fontSize: '0.7rem',
                color: 'text.primary',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {error && t(`common:${getErrorI18nKey(error.code)}`, {
                defaultValue: t('common:status.error'),
              })}
            </Typography>
          </Stack>
        </InlineErrorBar>
      </Collapse>

      {/* Expanded state: full button */}
      <Collapse in={!connectionButtonCollapsed} timeout={300}>
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            py: 2,
            px: 2,
          }}
        >
          <ConnectionButton
            serviceState={serviceState}
            hasTunnelSelected={hasTunnelSelected}
            tunnelName={tunnelName}
            tunnelCountry={tunnelCountry}
            onToggle={onToggle}
            isRetrying={isRetrying}
            networkAvailable={networkAvailable}
          />
        </Box>
      </Collapse>

      {/* 折叠/展开切换按钮 */}
      <CollapseToggle onClick={toggleConnectionButtonCollapsed}>
        <ToggleIconButton size="small" disableRipple>
          {connectionButtonCollapsed ? <ExpandMoreIcon /> : <ExpandLessIcon />}
        </ToggleIconButton>
      </CollapseToggle>
    </Box>
  );
}

export default CollapsibleConnectionSection;