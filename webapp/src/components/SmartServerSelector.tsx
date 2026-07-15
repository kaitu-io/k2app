import React from 'react';
import { Box, IconButton, Tab, Tabs, Tooltip } from '@mui/material';
import { Refresh as RefreshIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';
import { getCurrentAppConfig } from '../config/apps';

type TabValue = 'manual' | 'self_hosted' | 'k2sub';

interface Props {
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** 指定服务器 tab content (non-gateway only). */
  manualContent: React.ReactNode;
  /** 自部署 tab content. */
  selfHostedContent: React.ReactNode;
  /** K2sub (subscription) tab content (gateway only). */
  k2subContent: React.ReactNode;
  /**
   * Invoked when the user clicks the refresh icon on the 指定服务器 tab.
   * Parent owns the spinner state and surfaces failures (e.g. Snackbar).
   * Button is not rendered when omitted, on gateway, or on non-manual tabs.
   */
  onManualRefresh?: () => void;
  /** True while an async refresh is in flight — drives spinner + disables click. */
  manualRefreshing?: boolean;
}

// Filename retained from the smart-mode era. Gateway now exposes a `k2sub` tab
// (daemon-resolved subscription) in place of `manual`; non-gateway keeps `manual`.
export function SmartServerSelector({
  isInteractive,
  manualContent,
  selfHostedContent,
  k2subContent,
  onManualRefresh,
  manualRefreshing,
}: Props) {
  const { t } = useTranslation('dashboard');
  const serverMode = useConnectionStore((s) => s.serverMode);
  const setServerMode = useConnectionStore((s) => s.setServerMode);
  const isGateway = window._platform?.platformType === 'gateway';
  // Brands without a k2s install channel have no self-hosted surface at all.
  const selfHostedEnabled = getCurrentAppConfig().features.selfHostedTunnels === true;

  // A persisted serverMode='self_hosted' (brand switch, stale storage) must not
  // resolve to a tab that no longer exists — MUI would warn and render nothing.
  const tabValue: TabValue =
    serverMode === 'self_hosted' && selfHostedEnabled ? 'self_hosted'
    : serverMode === 'k2sub' ? 'k2sub'
    : isGateway ? 'k2sub'
    : 'manual';

  const handleTabChange = (_: React.SyntheticEvent, value: TabValue) => {
    if (!isInteractive) return;
    setServerMode(value).catch((err) =>
      console.warn('[ServerSelector] setServerMode failed:', err)
    );
  };

  const showManualRefresh = !isGateway && tabValue === 'manual' && onManualRefresh !== undefined;

  return (
    <Box>
      <Box
        sx={{
          position: 'sticky',
          top: 0,
          zIndex: 1,
          bgcolor: 'background.default',
          display: 'flex',
          alignItems: 'center',
        }}
      >
        <Tabs
          value={tabValue}
          onChange={handleTabChange}
          sx={{ mb: 0.5, minHeight: 36, flexGrow: 1 }}
          TabIndicatorProps={{ style: { height: 2 } }}
        >
          {isGateway ? (
            <Tab
              value="k2sub"
              label={t('serverSelector.tabSmart')}
              sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
              disabled={!isInteractive}
            />
          ) : (
            <Tab
              value="manual"
              label={t('serverSelector.tabManual')}
              sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
              disabled={!isInteractive}
            />
          )}
          {selfHostedEnabled && (
            <Tab
              value="self_hosted"
              label={t('serverSelector.tabSelfHosted')}
              sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
              disabled={!isInteractive}
            />
          )}
        </Tabs>
        {showManualRefresh && (
          <Tooltip title={t('dashboard.manualRefresh') || 'Refresh'}>
            <span>
              <IconButton
                size="small"
                data-testid="manual-refresh-button"
                onClick={onManualRefresh}
                disabled={manualRefreshing}
                sx={{ mr: 1, p: 0.5 }}
              >
                <RefreshIcon
                  sx={{
                    fontSize: 18,
                    animation: manualRefreshing ? 'spin 1s linear infinite' : 'none',
                    '@keyframes spin': {
                      '0%': { transform: 'rotate(0deg)' },
                      '100%': { transform: 'rotate(360deg)' },
                    },
                  }}
                />
              </IconButton>
            </span>
          </Tooltip>
        )}
      </Box>

      {/* manualContent (CloudTunnelList) stays mounted in gateway mode too —
          its onTunnelsLoaded callback feeds the country list inside k2subContent.
          In gateway mode tabValue is never 'manual', so it's always hidden. */}
      <Box sx={{ display: !isGateway && tabValue === 'manual' ? 'block' : 'none' }}>
        {manualContent}
      </Box>
      {isGateway && (
        <Box sx={{ display: tabValue === 'k2sub' ? 'block' : 'none' }}>
          {k2subContent}
        </Box>
      )}
      {selfHostedEnabled && (
        <Box sx={{ display: tabValue === 'self_hosted' ? 'block' : 'none' }}>
          {selfHostedContent}
        </Box>
      )}
    </Box>
  );
}
