/**
 * ServiceAlert - 原生样式的服务状态提示 Banner
 *
 * 显示场景：
 * 1. 服务组件初始化中
 * 2. Service 连接失败超过 10 秒
 * 3. VPN 连接出现 100 系列网络错误
 */

import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useVPNStatus, useVPNStore } from '../stores';
import { isNetworkError } from '../services/control-types';

interface ServiceAlertProps {
  sidebarWidth?: number;
}

// Alert type determines banner styling
type AlertType = 'initialization' | 'serviceFailure' | 'networkError';

// Theme colors for each alert type
const ALERT_THEMES = {
  initialization: {
    bg: '#EFF6FF',
    border: '#BFDBFE',
    icon: '#2563EB',
    text: '#1E40AF',
  },
  serviceFailure: {
    bg: '#FEF2F2',
    border: '#FECACA',
    icon: '#DC2626',
    text: '#991B1B',
  },
  networkError: {
    bg: '#FEF2F2',
    border: '#FECACA',
    icon: '#DC2626',
    text: '#991B1B',
  },
} as const;

function InfoIcon({ color }: { color: string }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
      <circle cx="8" cy="8" r="7" fill={color} />
      <path d="M8 4V8" stroke="white" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="8" cy="11" r="0.75" fill="white" />
    </svg>
  );
}

function WarningIcon({ color }: { color: string }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
      <path
        d="M8 1.5L14.5 13H1.5L8 1.5Z"
        fill={color}
        stroke={color}
        strokeWidth="1"
        strokeLinejoin="round"
      />
      <path d="M8 6V9" stroke="white" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="8" cy="11" r="0.75" fill="white" />
    </svg>
  );
}

export default function ServiceAlert({ sidebarWidth = 0 }: ServiceAlertProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { isServiceFailedLongTime, error } = useVPNStatus();
  const initialization = useVPNStore((state) => state.status?.initialization);
  const [isResolving, setIsResolving] = useState(false);

  // Handle resolve button click - try to reinstall service with admin privileges
  const handleResolve = async () => {
    const platform = window._platform;
    if (!platform?.reinstallService) {
      // Fallback to more page if not available (web mode)
      navigate('/service-error', { state: { from: location.pathname } });
      return;
    }

    setIsResolving(true);
    try {
      await platform.reinstallService();
      // Success - the service should restart and connection should recover
      // The alert will disappear automatically when service reconnects
    } catch (err: any) {
      // User cancelled or error occurred - navigate to more options
      if (err?.message !== 'User cancelled') {
        console.error('Failed to reinstall service:', err);
      }
      navigate('/service-error', { state: { from: location.pathname } });
    } finally {
      setIsResolving(false);
    }
  };

  // Determine alert type (priority: initialization > service failure > network error)
  const isInitializing = initialization && !initialization.ready;
  const hasNetworkError = error && isNetworkError(error.code);

  let alertType: AlertType | null = null;
  if (isInitializing) {
    alertType = 'initialization';
  } else if (isServiceFailedLongTime) {
    alertType = 'serviceFailure';
  } else if (hasNetworkError) {
    alertType = 'networkError';
  }

  if (!alertType) {
    return null;
  }

  // Get loading components list (only for initialization)
  const loadingComponents = isInitializing
    ? [
        initialization.geoip.loading && t('dashboard:dashboard.initialization.geoip'),
        initialization.rules.loading && t('dashboard:dashboard.initialization.rules'),
        initialization.antiblock.loading && t('dashboard:dashboard.initialization.antiblock'),
      ].filter(Boolean) as string[]
    : [];

  // Get title based on alert type
  const titleKeys = {
    initialization: 'dashboard:dashboard.initialization.title',
    serviceFailure: 'dashboard:dashboard.serviceFailure.title',
    networkError: 'dashboard:dashboard.networkError.title',
  };
  const title = t(titleKeys[alertType]);

  // Get theme colors
  const theme = ALERT_THEMES[alertType];
  const showResolveButton = alertType !== 'initialization';
  const Icon = alertType === 'initialization' ? InfoIcon : WarningIcon;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: sidebarWidth,
        right: 0,
        zIndex: 1100,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '8px 16px',
        backgroundColor: `var(--service-alert-bg, ${theme.bg})`,
        borderBottom: `1px solid var(--service-alert-border, ${theme.border})`,
        gap: '12px',
        minHeight: '36px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flex: 1 }}>
        <Icon color={theme.icon} />

        <div style={{ flex: 1 }}>
          <span
            style={{
              fontSize: '13px',
              fontWeight: 500,
              color: `var(--service-alert-text, ${theme.text})`,
              letterSpacing: '-0.01em',
            }}
          >
            {title}
          </span>
          {loadingComponents.length > 0 && (
            <span
              style={{
                fontSize: '12px',
                fontWeight: 400,
                color: `var(--service-alert-text, ${theme.text})`,
                opacity: 0.8,
                marginLeft: '8px',
              }}
            >
              ({loadingComponents.join(', ')})
            </span>
          )}
        </div>
      </div>

      {showResolveButton && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <button
            onClick={handleResolve}
            disabled={isResolving}
            style={{
              padding: '4px 12px',
              fontSize: '12px',
              fontWeight: 500,
              color: 'white',
              backgroundColor: theme.icon,
              border: 'none',
              borderRadius: '4px',
              cursor: isResolving ? 'wait' : 'pointer',
              whiteSpace: 'nowrap',
              opacity: isResolving ? 0.7 : 1,
            }}
          >
            {isResolving
              ? t('dashboard:dashboard.serviceFailure.resolving', 'Resolving...')
              : t('dashboard:dashboard.serviceFailure.resolve', 'Resolve')}
          </button>
          <button
            onClick={() => navigate('/service-error', { state: { from: location.pathname } })}
            style={{
              padding: '0',
              fontSize: '13px',
              fontWeight: 400,
              color: `var(--service-alert-link, ${theme.text})`,
              backgroundColor: 'transparent',
              border: 'none',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
              textDecoration: 'none',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.textDecoration = 'underline';
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.textDecoration = 'none';
            }}
          >
            {t('dashboard:dashboard.serviceFailure.more', 'More')}
          </button>
        </div>
      )}
    </div>
  );
}
