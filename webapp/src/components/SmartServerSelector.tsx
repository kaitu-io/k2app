import React, { useMemo } from 'react';
import {
  Box,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Radio,
  Skeleton,
  Tab,
  Tabs,
} from '@mui/material';
import { SmartModeIcon } from './SmartModeIcon';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';
import type { Tunnel } from '../services/api-types';
import { getFlagIcon, getCountryName } from '../utils/country';
import { useTheme } from '@mui/material/styles';

interface Props {
  /** Tunnel list from Dashboard (already fetched via CloudTunnelList). */
  tunnels: Tunnel[];
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** 指定服务器 tab content (CloudTunnelList without its header). */
  manualContent: React.ReactNode;
  /** 自部署 tab content. */
  selfHostedContent: React.ReactNode;
}

function buildCountryList(tunnels: Tunnel[]): string[] {
  const counts: Record<string, number> = {};
  for (const t of tunnels) {
    const code = (t.node.country ?? '').toLowerCase();
    if (code) counts[code] = (counts[code] ?? 0) + 1;
  }
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1])
    .map(([code]) => code);
}

export function SmartServerSelector({ tunnels, isInteractive, manualContent, selfHostedContent }: Props) {
  const { t } = useTranslation('dashboard');
  const theme = useTheme();
  const serverMode = useConnectionStore(s => s.serverMode);
  const smartCountry = useConnectionStore(s => s.smartCountry);
  const setServerMode = useConnectionStore(s => s.setServerMode);
  const setSmartCountry = useConnectionStore(s => s.setSmartCountry);

  const countries = useMemo(() => buildCountryList(tunnels), [tunnels]);

  const handleTabChange = (_: React.SyntheticEvent, value: 'smart' | 'manual' | 'self_hosted') => {
    if (!isInteractive) return;
    setServerMode(value).catch(err => console.warn('[SmartServerSelector] setServerMode failed:', err));
  };

  const handleCountrySelect = (code: string | null) => {
    if (!isInteractive) return;
    setSmartCountry(code).catch(err => console.warn('[SmartServerSelector] setSmartCountry failed:', err));
  };

  const selectedBg = theme.palette.mode === 'dark'
    ? 'rgba(255,255,255,0.06)'
    : 'rgba(0,0,0,0.04)';

  const rowSx = (selected: boolean) => ({
    borderRadius: 2,
    minHeight: 56,
    cursor: isInteractive ? 'pointer' : 'default',
    bgcolor: selected ? selectedBg : undefined,
    transition: 'background 0.15s',
    '&:hover': isInteractive ? { bgcolor: selected ? selectedBg : 'action.hover' } : {},
  });

  const tabValue: 'smart' | 'manual' | 'self_hosted' =
    serverMode === 'self_hosted' ? 'self_hosted' :
    serverMode === 'manual' ? 'manual' :
    'smart';

  return (
    <Box>
      {/* ── Mode tabs ─────────────────────────────────────────── */}
      <Box sx={{ position: 'sticky', top: 0, zIndex: 1, bgcolor: 'background.default' }}>
      <Tabs
        value={tabValue}
        onChange={handleTabChange}
        sx={{ mb: 0.5, minHeight: 36 }}
        TabIndicatorProps={{ style: { height: 2 } }}
      >
        <Tab
          value="smart"
          label={t('serverSelector.tabSmart')}
          sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
          disabled={!isInteractive}
        />
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

      {/* All panels always mounted — display toggle preserves component state */}

      {/* ── Smart tab ─────────────────────────────────────────── */}
      <Box sx={{ display: tabValue === 'smart' ? 'block' : 'none' }}>
        <List disablePadding sx={{ px: 2 }}>
          {/* 自动 — always first */}
          <ListItem
            disableGutters
            onClick={() => handleCountrySelect(null)}
            sx={rowSx(smartCountry === null)}
          >
            <ListItemIcon sx={{ minWidth: 40 }}>
              <SmartModeIcon />
            </ListItemIcon>
            <ListItemText
              primary={t('serverSelector.countryAuto')}
              secondary={t('serverSelector.smartHint')}
              primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
              secondaryTypographyProps={{ fontSize: '0.72rem' }}
            />
            <Radio
              checked={smartCountry === null}
              color="primary"
              size="small"
              sx={{ '& .MuiSvgIcon-root': { fontSize: 22 } }}
            />
          </ListItem>

          {/* One row per country */}
          {countries.map(code => (
            <ListItem
              key={code}
              disableGutters
              onClick={() => handleCountrySelect(code)}
              sx={rowSx(smartCountry === code)}
            >
              <ListItemIcon sx={{ minWidth: 40 }}>
                {getFlagIcon(code)}
              </ListItemIcon>
              <ListItemText
                primary={getCountryName(code)}
                primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
              />
              <Radio
                checked={smartCountry === code}
                color="primary"
                size="small"
                sx={{ '& .MuiSvgIcon-root': { fontSize: 22 } }}
              />
            </ListItem>
          ))}

          {countries.length === 0 && tunnels.length === 0 && (
            // Data not loaded yet — show skeleton country rows
            <>
              {[1, 2, 3].map(i => (
                <ListItem key={i} disableGutters sx={{ minHeight: 56 }}>
                  <ListItemIcon sx={{ minWidth: 40 }}>
                    <Skeleton variant="rounded" width={32} height={22} />
                  </ListItemIcon>
                  <ListItemText
                    primary={<Skeleton width={80} />}
                  />
                </ListItem>
              ))}
            </>
          )}
        </List>
      </Box>

      {/* ── Manual (指定服务器) tab ───────────────────────────── */}
      <Box sx={{ display: tabValue === 'manual' ? 'block' : 'none' }}>
        {manualContent}
      </Box>

      {/* ── Self-hosted tab ───────────────────────────────────── */}
      <Box sx={{ display: tabValue === 'self_hosted' ? 'block' : 'none' }}>
        {selfHostedContent}
      </Box>
    </Box>
  );
}
