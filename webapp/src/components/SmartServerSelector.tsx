import React, { useMemo } from 'react';
import {
  Box,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Radio,
  Tab,
  Tabs,
  Typography,
} from '@mui/material';
import PublicIcon from '@mui/icons-material/Public';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';
import type { Tunnel } from '../services/api-types';
import { getFlagIcon, getCountryName } from '../utils/country';
import { useTheme } from '@mui/material/styles';

interface Props {
  /** Tunnel list from Dashboard (already fetched via hidden CloudTunnelList). */
  tunnels: Tunnel[];
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** Self-hosted tab content. */
  selfHostedContent: React.ReactNode;
}

interface CountrySummary {
  code: string;
  count: number;
}

function buildCountrySummary(tunnels: Tunnel[]): CountrySummary[] {
  const counts: Record<string, number> = {};
  for (const t of tunnels) {
    const code = (t.node.country ?? '').toLowerCase();
    if (code) counts[code] = (counts[code] ?? 0) + 1;
  }
  return Object.entries(counts)
    .map(([code, count]) => ({ code, count }))
    .sort((a, b) => b.count - a.count);
}

export function SmartServerSelector({ tunnels, isInteractive, selfHostedContent }: Props) {
  const { t } = useTranslation('dashboard');
  const theme = useTheme();
  const serverMode = useConnectionStore(s => s.serverMode);
  const smartCountry = useConnectionStore(s => s.smartCountry);
  const setServerMode = useConnectionStore(s => s.setServerMode);
  const setSmartCountry = useConnectionStore(s => s.setSmartCountry);

  const countries = useMemo(() => buildCountrySummary(tunnels), [tunnels]);

  const handleTabChange = (_: React.SyntheticEvent, value: 'smart' | 'self_hosted') => {
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
    px: 1,
  });

  // Smart tab renders as a list, identical visual weight to the self-hosted list
  const tabValue: 'smart' | 'self_hosted' = serverMode === 'self_hosted' ? 'self_hosted' : 'smart';

  return (
    <Box>
      {/* ── Mode tabs ─────────────────────────────────────────── */}
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
          value="self_hosted"
          label={t('serverSelector.tabSelfHosted')}
          sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem' }}
          disabled={!isInteractive}
        />
      </Tabs>

      {/* ── Smart tab — list style, same as self-hosted ───────── */}
      {tabValue === 'smart' && (
        <List disablePadding sx={{ px: 1 }}>
          {/* 自动 — always first */}
          <ListItem
            disableGutters
            onClick={() => handleCountrySelect(null)}
            sx={rowSx(smartCountry === null)}
          >
            <ListItemIcon sx={{ minWidth: 40 }}>
              <Box sx={{
                width: 32, height: 22,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}>
                <PublicIcon sx={{ fontSize: 20, color: 'text.secondary' }} />
              </Box>
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
          {countries.map(c => (
            <ListItem
              key={c.code}
              disableGutters
              onClick={() => handleCountrySelect(c.code)}
              sx={rowSx(smartCountry === c.code)}
            >
              <ListItemIcon sx={{ minWidth: 40 }}>
                {getFlagIcon(c.code)}
              </ListItemIcon>
              <ListItemText
                primary={getCountryName(c.code)}
                secondary={`\u00d7${c.count}`}
                primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
                secondaryTypographyProps={{ fontSize: '0.72rem' }}
              />
              <Radio
                checked={smartCountry === c.code}
                color="primary"
                size="small"
                sx={{ '& .MuiSvgIcon-root': { fontSize: 22 } }}
              />
            </ListItem>
          ))}

          {countries.length === 0 && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', px: 1, py: 2 }}>
              {t('serverSelector.smartHint')}
            </Typography>
          )}
        </List>
      )}

      {/* ── Self-hosted tab ───────────────────────────────────── */}
      {tabValue === 'self_hosted' && selfHostedContent}
    </Box>
  );
}
