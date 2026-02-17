/**
 * ConnectionNotification - 连接状态通知组件
 *
 * 特性：
 * - 显示在右上角的紧凑通知
 * - 根据类型显示不同图标（info/warning/error）
 * - 100 系列错误码显示"修复网络"引导
 * - 自动淡入淡出动画
 */

import { useCallback } from 'react';
import {
  Box,
  Typography,
  IconButton,
  Button,
  Stack,
  styled,
  alpha,
} from '@mui/material';
import {
  Info as InfoIcon,
  Warning as WarningIcon,
  Error as ErrorIcon,
  Close as CloseIcon,
  Build as FixIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import type { ControlError } from '../services/control-types';
import { getErrorI18nKey } from '../services/control-types';

type NotificationType = 'info' | 'warning' | 'error';

interface NotificationConfig {
  type: NotificationType;
  showFixNetwork: boolean;
}

/**
 * 根据错误码判断通知类型和是否显示修复网络
 */
function getNotificationConfig(error: ControlError): NotificationConfig {
  const code = error.code;

  // 100 系列：网络错误，显示修复网络按钮
  if (code >= 100 && code <= 109) {
    return { type: 'warning', showFixNetwork: true };
  }

  // 110 系列：服务器错误
  if (code >= 110 && code <= 119) {
    return { type: 'error', showFixNetwork: false };
  }

  // 500 系列：VPN 服务错误
  if (code >= 510 && code <= 519) {
    return { type: 'error', showFixNetwork: false };
  }

  // 520 系列：网络修复相关错误
  if (code >= 520 && code <= 529) {
    return { type: 'warning', showFixNetwork: true };
  }

  // 570 系列：连接错误，可能是网络问题
  if (code >= 570 && code <= 579) {
    return { type: 'error', showFixNetwork: true };
  }

  // 其他错误
  return { type: 'error', showFixNetwork: false };
}

const NotificationContainer = styled(Box, {
  shouldForwardProp: (prop) => prop !== 'notificationType',
})<{ notificationType: NotificationType }>(({ theme, notificationType }) => {
  const getColor = () => {
    switch (notificationType) {
      case 'info':
        return theme.palette.info.main;
      case 'warning':
        return theme.palette.warning.main;
      case 'error':
        return theme.palette.error.main;
      default:
        return theme.palette.info.main;
    }
  };

  const color = getColor();

  return {
    position: 'absolute',
    top: 8,
    right: 8,
    maxWidth: 280,
    padding: '6px 10px',
    borderRadius: 8,
    backgroundColor: alpha(color, theme.palette.mode === 'dark' ? 0.15 : 0.1),
    border: `1px solid ${alpha(color, 0.3)}`,
    display: 'flex',
    alignItems: 'flex-start',
    gap: 6,
    zIndex: 10,
    animation: 'slideIn 0.3s ease-out',
    '@keyframes slideIn': {
      from: {
        opacity: 0,
        transform: 'translateX(20px)',
      },
      to: {
        opacity: 1,
        transform: 'translateX(0)',
      },
    },
  };
});

const IconWrapper = styled(Box, {
  shouldForwardProp: (prop) => prop !== 'notificationType',
})<{ notificationType: NotificationType }>(({ theme, notificationType }) => {
  const getColor = () => {
    switch (notificationType) {
      case 'info':
        return theme.palette.info.main;
      case 'warning':
        return theme.palette.warning.main;
      case 'error':
        return theme.palette.error.main;
      default:
        return theme.palette.info.main;
    }
  };

  return {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    marginTop: 2,
    color: getColor(),
  };
});

export interface ConnectionNotificationProps {
  /** 连接错误信息 */
  error: ControlError | null;
  /** 关闭回调 */
  onClose?: () => void;
}

export function ConnectionNotification({
  error,
  onClose,
}: ConnectionNotificationProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const handleFixNetwork = useCallback(() => {
    navigate('/faq');
  }, [navigate]);

  if (!error) {
    return null;
  }

  const config = getNotificationConfig(error);

  const NotificationIcon = () => {
    switch (config.type) {
      case 'info':
        return <InfoIcon sx={{ fontSize: 16 }} />;
      case 'warning':
        return <WarningIcon sx={{ fontSize: 16 }} />;
      case 'error':
        return <ErrorIcon sx={{ fontSize: 16 }} />;
      default:
        return <InfoIcon sx={{ fontSize: 16 }} />;
    }
  };

  return (
    <NotificationContainer notificationType={config.type}>
      <IconWrapper notificationType={config.type}>
        <NotificationIcon />
      </IconWrapper>

      <Stack spacing={0.5} sx={{ flex: 1, minWidth: 0 }}>
        <Typography
          variant="caption"
          sx={{
            fontSize: '0.7rem',
            lineHeight: 1.3,
            color: 'text.primary',
            wordBreak: 'break-word',
          }}
        >
          {t(`common:${getErrorI18nKey(error.code)}`, {
            defaultValue: error.message || t('common:status.error'),
          })}
        </Typography>

        {config.showFixNetwork && (
          <Button
            size="small"
            startIcon={<FixIcon sx={{ fontSize: 12 }} />}
            onClick={handleFixNetwork}
            sx={{
              fontSize: '0.65rem',
              py: 0.25,
              px: 1,
              minHeight: 'auto',
              textTransform: 'none',
              alignSelf: 'flex-start',
            }}
          >
            {t('dashboard:troubleshooting.fixNetwork.button') || 'Fix Network'}
          </Button>
        )}
      </Stack>

      {onClose && (
        <IconButton
          size="small"
          onClick={onClose}
          sx={{
            p: 0.25,
            ml: 0.5,
            flexShrink: 0,
          }}
        >
          <CloseIcon sx={{ fontSize: 14 }} />
        </IconButton>
      )}
    </NotificationContainer>
  );
}

export default ConnectionNotification;
