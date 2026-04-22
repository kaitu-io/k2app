import React from 'react';
import { Box, IconButton, Tab, Tabs, Tooltip } from '@mui/material';
import { Refresh as RefreshIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';

interface Props {
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** 指定服务器 tab content. */
  manualContent: React.ReactNode;
  /** 自部署 tab content. */
  selfHostedContent: React.ReactNode;
  /**
   * Invoked when the user clicks the refresh icon on the 指定服务器 tab.
   * Parent owns the spinner state and surfaces failures (e.g. Snackbar).
   * Button is not rendered when omitted.
   */
  onManualRefresh?: () => void;
  /** True while an async refresh is in flight — drives spinner + disables click. */
  manualRefreshing?: boolean;
}

// Filename retained from the smart-mode era; component now only switches
// between 'manual' (cloud tunnel list) and 'self_hosted'.
export function SmartServerSelector({
  isInteractive,
  manualContent,
  selfHostedContent,
  onManualRefresh,
  manualRefreshing,
}: Props) {
  const { t } = useTranslation('dashboard');
  const serverMode = useConnectionStore((s) => s.serverMode);
  const setServerMode = useConnectionStore((s) => s.setServerMode);

  const handleTabChange = (_: React.SyntheticEvent, value: 'manual' | 'self_hosted') => {
    if (!isInteractive) return;
    setServerMode(value).catch((err) =>
      console.warn('[ServerSelector] setServerMode failed:', err)
    );
  };

  const tabValue: 'manual' | 'self_hosted' =
    serverMode === 'self_hosted' ? 'self_hosted' : 'manual';

  const showManualRefresh = tabValue === 'manual' && onManualRefresh !== undefined;

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
          <Tab
            value="manual"
            label={t('serverSelector.tabManual')}
            sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
            disabled={!isInteractive}
          />
          <Tab
            value="self_hosted"
            label={t('serverSelector.tabSelfHosted')}
            sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
            disabled={!isInteractive}
          />
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

      <Box sx={{ display: tabValue === 'manual' ? 'block' : 'none' }}>
        {manualContent}
      </Box>
      <Box sx={{ display: tabValue === 'self_hosted' ? 'block' : 'none' }}>
        {selfHostedContent}
      </Box>
    </Box>
  );
}
