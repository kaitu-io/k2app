import React from 'react';
import { Box, Tab, Tabs } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';

interface Props {
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** 指定服务器 tab content. */
  manualContent: React.ReactNode;
  /** 自部署 tab content. */
  selfHostedContent: React.ReactNode;
}

// Filename retained from the smart-mode era; component now only switches
// between 'manual' (cloud tunnel list) and 'self_hosted'.
export function SmartServerSelector({ isInteractive, manualContent, selfHostedContent }: Props) {
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

  return (
    <Box>
      <Box sx={{ position: 'sticky', top: 0, zIndex: 1, bgcolor: 'background.default' }}>
        <Tabs
          value={tabValue}
          onChange={handleTabChange}
          sx={{ mb: 0.5, minHeight: 36 }}
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
