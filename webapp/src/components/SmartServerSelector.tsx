import React, { useMemo } from 'react';
import {
  Box,
  Chip,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
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
  /** Manual tab content: CloudTunnelList + self-hosted section. */
  children: React.ReactNode;
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


export function SmartServerSelector({ tunnels, isInteractive, children }: Props) {
  const { t } = useTranslation('dashboard');
  const serverMode = useConnectionStore(s => s.serverMode);
  const smartCountry = useConnectionStore(s => s.smartCountry);
  const setServerMode = useConnectionStore(s => s.setServerMode);
  const setSmartCountry = useConnectionStore(s => s.setSmartCountry);

  const countries = useMemo(() => buildCountrySummary(tunnels), [tunnels]);

  const handleTabChange = (_: React.SyntheticEvent, value: 'smart' | 'manual') => {
    if (!isInteractive) return;
    setServerMode(value).catch(err => console.warn('[SmartServerSelector] setServerMode failed:', err));
  };

  const handleCountryChange = async (value: string) => {
    if (!isInteractive) return;
    await setSmartCountry(value === '' ? null : value);
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
          sx={{ minHeight: 36, py: 0.5, fontSize: '0.8rem', opacity: serverMode === 'smart' ? 0.65 : 1 }}
          disabled={!isInteractive}
        />
      </Tabs>

      {/* ── Smart tab ─────────────────────────────────────────── */}
      {serverMode === 'smart' && (
        <Box sx={{ px: 0.5 }}>
          {/* Country picker */}
          <FormControl size="small" fullWidth sx={{ mb: 1.5 }} disabled={!isInteractive}>
            <InputLabel id="smart-country-label">
              {t('serverSelector.countryPickerLabel')}
            </InputLabel>
            <Select
              labelId="smart-country-label"
              value={smartCountry ?? ''}
              label={t('serverSelector.countryPickerLabel')}
              onChange={e => handleCountryChange(e.target.value)}
            >
              <MenuItem value="">
                <em>{t('serverSelector.countryAuto')}</em>
              </MenuItem>
              {countries.map(c => (
                <MenuItem key={c.code} value={c.code}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    {getFlagIcon(c.code)}
                    <span>
                      {getCountryName(c.code)} &times;{c.count}
                    </span>
                  </Box>
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          {/* Country chip overview */}
          {countries.length > 0 && (
            <>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.75 }}>
                {t('serverSelector.nodesAvailable', {
                  count: tunnels.length,
                  regions: countries.length,
                })}
              </Typography>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mb: 1.5 }}>
                {countries.map(c => (
                  <Chip
                    key={c.code}
                    size="small"
                    icon={<Box sx={{ display: 'flex', pl: 0.5 }}>{getFlagIcon(c.code)}</Box>}
                    label={`${getCountryName(c.code)} \u00d7${c.count}`}
                    onClick={isInteractive ? () => handleCountryChange(c.code) : undefined}
                    variant={smartCountry === c.code ? 'filled' : 'outlined'}
                    color={smartCountry === c.code ? 'primary' : 'default'}
                    sx={{
                      fontSize: '0.72rem',
                      cursor: isInteractive ? 'pointer' : 'default',
                    }}
                  />
                ))}
              </Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                {t('serverSelector.smartHint')}
              </Typography>
            </>
          )}
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
    </Box>
  );
}
