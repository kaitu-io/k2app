/**
 * RoutingModeSelector — unified routing preset control inside Advanced Settings.
 *
 * Four presets as a RadioGroup:
 *   1. global     — all traffic proxied
 *   2. bypass     — country traffic direct, rest proxied
 *   3. home       — country traffic via home router, rest direct (disabled/coming soon)
 *   4. home_proxy — country traffic via home router, rest proxied (disabled/coming soon)
 *
 * When preset !== 'global': shows Country Select + AutoDetect checkbox.
 * All controls disabled when VPN is connected/connecting (isInteractive).
 */

import { useCallback } from 'react';
import {
  Box,
  Checkbox,
  Chip,
  FormControlLabel,
  MenuItem,
  Radio,
  RadioGroup,
  Select,
  Stack,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';

import { useConfigStore, type RoutePreset } from '../stores/config.store';
import { useVPNMachine } from '../stores/vpn-machine.store';
import {
  SUPPORTED_COUNTRY_CODES,
  countryFlagEmoji,
  countryName,
} from '../utils/countries';

// ---- Preset definitions ----

interface PresetOption {
  value: RoutePreset;
  emoji: string;
  labelKey: string;
  descKey: string;
  disabled: boolean;
}

const PRESET_OPTIONS: PresetOption[] = [
  { value: 'global',     emoji: '\uD83C\uDF0D', labelKey: 'presetGlobal',     descKey: 'presetGlobalDesc',     disabled: false },
  { value: 'bypass',     emoji: '\u26A1',        labelKey: 'presetBypass',     descKey: 'presetBypassDesc',     disabled: false },
  { value: 'home',       emoji: '\uD83C\uDFE0', labelKey: 'presetHome',       descKey: 'presetHomeDesc',       disabled: true },
  { value: 'home_proxy', emoji: '\uD83C\uDFE0', labelKey: 'presetHomeProxy',  descKey: 'presetHomeProxyDesc',  disabled: true },
];

// ---- Exported summary hook ----

/**
 * Returns a short summary for the collapsed advanced settings bar.
 * e.g. { label: 'Global Proxy', flag: '' } or { label: 'China Direct', flag: '...' }
 */
export function useRoutingSummary(): { label: string; flag: string } {
  const { t, i18n } = useTranslation('dashboard');
  const preset = useConfigStore((s) => s.resolvePreset());
  const country = useConfigStore((s) => s.country);
  const autoDetect = useConfigStore((s) => s.autoDetect);
  const detectedCountry = useConfigStore((s) => s.detectedCountry);

  const effectiveCountry = country || (autoDetect ? detectedCountry : null);
  const name = effectiveCountry ? countryName(effectiveCountry, i18n.language) : '';
  const flag = effectiveCountry ? countryFlagEmoji(effectiveCountry) : '';

  // When no country detected yet, show the preset label (e.g. "智能分流") instead
  // of an incomplete summary like "直连" (missing country name).
  if (preset !== 'global' && !name) {
    const presetLabelKey = preset === 'bypass' ? 'presetBypass'
      : preset === 'home' ? 'presetHome' : 'presetHomeProxy';
    return { label: t(`smartMode.${presetLabelKey}`), flag: '' };
  }

  switch (preset) {
    case 'global':
      return { label: t('smartMode.summaryGlobal'), flag: '' };
    case 'bypass':
      return { label: t('smartMode.summaryBypass', { country: name }), flag };
    case 'home':
      return { label: t('smartMode.summaryHome', { country: name }), flag };
    case 'home_proxy':
      return { label: t('smartMode.summaryHomeProxy', { country: name }), flag };
  }
}

// ---- Main component ----

export default function RoutingModeSelector() {
  const { t, i18n } = useTranslation('dashboard');

  const preset = useConfigStore((s) => s.resolvePreset());
  const country = useConfigStore((s) => s.country);
  const autoDetect = useConfigStore((s) => s.autoDetect);
  const detectedCountry = useConfigStore((s) => s.detectedCountry);
  const setPreset = useConfigStore((s) => s.setPreset);
  const setCountry = useConfigStore((s) => s.setCountry);
  const setAutoDetect = useConfigStore((s) => s.setAutoDetect);

  const { isInteractive } = useVPNMachine();

  // Resolve display country for Select value
  const displayCountry = country
    || (autoDetect && detectedCountry ? detectedCountry : '')
    || '';

  const handlePresetChange = useCallback(
    (_: React.ChangeEvent<HTMLInputElement>, value: string) => {
      setPreset(value as RoutePreset);
    },
    [setPreset],
  );

  const handleCountryChange = useCallback(
    (e: { target: { value: string } }) => {
      setCountry(e.target.value);
    },
    [setCountry],
  );

  const handleAutoDetectToggle = useCallback(
    (_: React.ChangeEvent<HTMLInputElement>, checked: boolean) => {
      setAutoDetect(checked);
    },
    [setAutoDetect],
  );

  const showCountryControls = preset !== 'global';

  return (
    <Box data-testid="routing-mode-selector">
      {/* Section header */}
      <Typography variant="body2" fontWeight={600} sx={{ mb: 1 }}>
        {t('smartMode.routingMode')}
      </Typography>

      {/* Disabled warning when VPN is active */}
      {isInteractive && (
        <Typography variant="caption" color="warning.main" sx={{ display: 'block', mb: 1 }}>
          {t('dashboard.disconnectToModify')}
        </Typography>
      )}

      {/* Preset radio group */}
      <RadioGroup
        value={preset}
        onChange={handlePresetChange}
        data-testid="routing-preset-group"
      >
        {PRESET_OPTIONS.map((opt) => {
          const isDisabled = isInteractive || opt.disabled;
          const localizedCountry = displayCountry
            ? countryName(displayCountry, i18n.language)
            : '';
          const description = t(`smartMode.${opt.descKey}`, { country: localizedCountry });

          return (
            <FormControlLabel
              key={opt.value}
              value={opt.value}
              disabled={isDisabled}
              data-testid={`routing-preset-${opt.value}`}
              control={<Radio size="small" />}
              label={
                <Box sx={{ ml: 0.5 }}>
                  <Stack direction="row" alignItems="center" spacing={0.5}>
                    <Typography variant="body2" sx={{ fontSize: '0.85rem' }}>
                      {opt.emoji} {t(`smartMode.${opt.labelKey}`)}
                    </Typography>
                    {opt.disabled && (
                      <Chip
                        label={t('smartMode.comingSoon')}
                        size="small"
                        variant="outlined"
                        sx={{ height: 18, fontSize: '0.65rem' }}
                      />
                    )}
                  </Stack>
                  <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                    {description}
                  </Typography>
                </Box>
              }
              sx={{ alignItems: 'flex-start', mb: 0.5, '& .MuiRadio-root': { pt: 0.5 } }}
            />
          );
        })}
      </RadioGroup>

      {/* Country selection + auto-detect -- only when preset needs a country */}
      {showCountryControls && (
        <Stack spacing={1} sx={{ mt: 1 }}>
          <Typography variant="body2" fontWeight={600}>
            {t('smartMode.countryLabel')}
          </Typography>

          <Select
            value={displayCountry}
            onChange={handleCountryChange}
            disabled={isInteractive}
            size="small"
            fullWidth
            displayEmpty
            data-testid="country-select"
            renderValue={(value) => {
              if (!value) {
                return (
                  <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.85rem' }}>
                    {autoDetect
                      ? t('smartMode.autoDetecting')
                      : t('smartMode.selectCountry')}
                  </Typography>
                );
              }
              const flag = countryFlagEmoji(value);
              const name = countryName(value, i18n.language);
              return (
                <Typography variant="body2" sx={{ fontSize: '0.85rem' }}>
                  {flag} {name}
                </Typography>
              );
            }}
          >
            {SUPPORTED_COUNTRY_CODES.map((cc) => (
              <MenuItem key={cc} value={cc} data-testid={`country-option-${cc}`}>
                <Typography variant="body2" sx={{ fontSize: '0.85rem' }}>
                  {countryFlagEmoji(cc)} {countryName(cc, i18n.language)}
                </Typography>
              </MenuItem>
            ))}
          </Select>

          <FormControlLabel
            control={
              <Checkbox
                checked={autoDetect}
                onChange={handleAutoDetectToggle}
                disabled={isInteractive}
                size="small"
                data-testid="auto-detect-checkbox"
              />
            }
            label={
              <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
                {t('smartMode.autoDetectLabel')}
              </Typography>
            }
          />
        </Stack>
      )}
    </Box>
  );
}
