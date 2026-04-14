import React, { useMemo } from 'react';
import {
  Box,
  Chip,
  Tab,
  Tabs,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';
import type { Tunnel } from '../services/api-types';
import { getFlagIcon, getCountryName } from '../utils/country';

interface Props {
  /** Tunnel list from Dashboard (already fetched by CloudTunnelList). */
  tunnels: Tunnel[];
  /** False when VPN is connecting/connected — disables mode switching. */
  isInteractive: boolean;
  /** Manual tab content: CloudTunnelList only (no self-hosted). */
  children: React.ReactNode;
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

export function SmartServerSelector({ tunnels, isInteractive, children, selfHostedContent }: Props) {
  const { t } = useTranslation('dashboard');
  const serverMode = useConnectionStore(s => s.serverMode);
  const smartCountry = useConnectionStore(s => s.smartCountry);
  const setServerMode = useConnectionStore(s => s.setServerMode);
  const setSmartCountry = useConnectionStore(s => s.setSmartCountry);

  const countries = useMemo(() => buildCountrySummary(tunnels), [tunnels]);

  const handleTabChange = (_: React.SyntheticEvent, value: 'smart' | 'manual' | 'self_hosted') => {
    if (!isInteractive) return;
    setServerMode(value).catch(err => console.warn('[SmartServerSelector] setServerMode failed:', err));
  };

  const handleCountrySelect = (code: string | null) => {
    if (!isInteractive) return;
    setSmartCountry(code).catch(err => console.warn('[SmartServerSelector] setSmartCountry failed:', err));
  };

  return (
    <Box>
      {/* ── Mode tabs ─────────────────────────────────────────── */}
      <Tabs
        value={serverMode}
        onChange={handleTabChange}
        sx={{ mb: 1.5, minHeight: 36 }}
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

      {/* ── Smart tab — flat chip list, no dropdown ───────────── */}
      {serverMode === 'smart' && (
        <Box sx={{ px: 0.5 }}>
          {/* All options visible at once: 自动 + each country */}
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1.25 }}>
            {/* 自动 — always first */}
            <Chip
              size="small"
              label={t('serverSelector.countryAuto')}
              onClick={isInteractive ? () => handleCountrySelect(null) : undefined}
              variant={smartCountry === null ? 'filled' : 'outlined'}
              color={smartCountry === null ? 'primary' : 'default'}
              sx={{ fontSize: '0.72rem', cursor: isInteractive ? 'pointer' : 'default' }}
            />
            {/* One chip per country */}
            {countries.map(c => (
              <Chip
                key={c.code}
                size="small"
                icon={<Box sx={{ display: 'flex', pl: 0.5 }}>{getFlagIcon(c.code)}</Box>}
                label={`${getCountryName(c.code)} \u00d7${c.count}`}
                onClick={isInteractive ? () => handleCountrySelect(c.code) : undefined}
                variant={smartCountry === c.code ? 'filled' : 'outlined'}
                color={smartCountry === c.code ? 'primary' : 'default'}
                sx={{ fontSize: '0.72rem', cursor: isInteractive ? 'pointer' : 'default' }}
              />
            ))}
          </Box>

          {/* Summary + hint */}
          {countries.length > 0 && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
              {t('serverSelector.nodesAvailable', {
                count: tunnels.length,
                regions: countries.length,
              })}
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
            {t('serverSelector.smartHint')}
          </Typography>
        </Box>
      )}

      {/* ── Manual tab ────────────────────────────────────────── */}
      {serverMode === 'manual' && (
        <>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'block', px: 0.5, mb: 1 }}
          >
            {t('serverSelector.manualHint')}
          </Typography>
          {children}
        </>
      )}

      {/* ── Self-hosted tab ───────────────────────────────────── */}
      {serverMode === 'self_hosted' && selfHostedContent}
    </Box>
  );
}
