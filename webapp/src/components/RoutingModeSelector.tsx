/**
 * RoutingModeSelector — unified routing preset control inside Advanced Settings.
 *
 * Two presets as a RadioGroup:
 *   1. global  — all traffic proxied
 *   2. bypass  — country traffic direct, rest proxied
 *
 * When preset !== 'global' AND the brand does multi-country routing (Overleap):
 * shows Country Select + AutoDetect checkbox. Kaitu is China-market (region
 * always cn), so the country controls are hidden and bypass reads as "中国直连".
 * All controls disabled when VPN is connected/connecting (isInteractive).
 */

import { useCallback, useMemo, useState } from 'react';
import {
  Box,
  ButtonBase,
  Checkbox,
  FormControlLabel,
  Radio,
  RadioGroup,
  Stack,
  Typography,
} from '@mui/material';
import { Search as SearchIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

import { useConfigStore, type RoutePreset } from '../stores/config.store';
import { useVPNMachine } from '../stores/vpn-machine.store';
import {
  SUPPORTED_COUNTRY_CODES,
  countryFlagEmoji,
  countryName,
} from '../utils/countries';
import { getCurrentAppConfig } from '../config/apps';
import { brandConfig } from '../brands';
import SearchPickerDialog, { type PickerOption } from './SearchPickerDialog';

// ---- Preset definitions ----

interface PresetOption {
  value: RoutePreset;
  emoji: string;
  labelKey: string;
  descKey: string;
}

const PRESET_OPTIONS: PresetOption[] = [
  { value: 'global', emoji: '\uD83C\uDF0D', labelKey: 'presetGlobal', descKey: 'presetGlobalDesc' },
  { value: 'bypass', emoji: '\u26A1',        labelKey: 'presetBypass', descKey: 'presetBypassDesc' },
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

  const [pickerOpen, setPickerOpen] = useState(false);

  const handleCountryPick = useCallback(
    (cc: string) => {
      setPickerOpen(false);
      setCountry(cc);
    },
    [setCountry],
  );

  const handleAutoDetectToggle = useCallback(
    (_: React.ChangeEvent<HTMLInputElement>, checked: boolean) => {
      setAutoDetect(checked);
    },
    [setAutoDetect],
  );

  // 品牌默认国家置顶，其余按当前语言的名称排序。
  const countryOptions = useMemo(() => {
    const top = brandConfig.defaultRoutingCountry;
    return [...SUPPORTED_COUNTRY_CODES].sort((a, b) => {
      if (a === top) return -1;
      if (b === top) return 1;
      return countryName(a, i18n.language).localeCompare(countryName(b, i18n.language), i18n.language);
    });
  }, [i18n.language]);

  // 按当前语言名、英文名、两位代码都能搜到。
  const getCountryOptions = useCallback(
    (query: string): PickerOption[] => {
      const q = query.trim().toLowerCase();
      return countryOptions
        .filter((cc) =>
          !q
          || cc === q
          || countryName(cc, i18n.language).toLowerCase().includes(q)
          || countryName(cc, 'en').toLowerCase().includes(q))
        .map((cc) => ({
          id: cc,
          primary: `${countryFlagEmoji(cc)} ${countryName(cc, i18n.language)}`,
          secondary: countryName(cc, 'en') === countryName(cc, i18n.language)
            ? undefined
            : countryName(cc, 'en'),
        }));
    },
    [countryOptions, i18n.language],
  );

  const multiCountry = getCurrentAppConfig().features.multiCountryRouting === true;
  const showCountryControls = preset !== 'global' && multiCountry;

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
          const localizedCountry = displayCountry
            ? countryName(displayCountry, i18n.language)
            : '';
          const description = t(`smartMode.${opt.descKey}`, { country: localizedCountry });

          return (
            <FormControlLabel
              key={opt.value}
              value={opt.value}
              disabled={isInteractive}
              data-testid={`routing-preset-${opt.value}`}
              control={<Radio size="small" />}
              label={
                <Box sx={{ ml: 0.5 }}>
                  <Stack direction="row" alignItems="center" spacing={0.5}>
                    <Typography variant="body2" sx={{ fontSize: '0.85rem' }}>
                      {opt.emoji} {t(`smartMode.${opt.labelKey}`)}
                    </Typography>
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

          {/* 点开是一个顶部对齐的搜索弹窗（SearchPickerDialog），而不是挂在输入框
              下面的下拉：下拉会被手机键盘盖住，盖住的那几行点不到。 */}
          <ButtonBase
            onClick={() => setPickerOpen(true)}
            disabled={isInteractive}
            data-testid="country-select"
            aria-label={t('smartMode.countryLabel')}
            sx={{
              width: '100%',
              justifyContent: 'space-between',
              px: 1.5,
              py: 1,
              borderRadius: 1,
              border: 1,
              borderColor: 'divider',
              opacity: isInteractive ? 0.5 : 1,
            }}
          >
            <Typography
              variant="body2"
              color={displayCountry ? 'text.primary' : 'text.secondary'}
              sx={{ fontSize: '0.85rem' }}
            >
              {displayCountry
                ? `${countryFlagEmoji(displayCountry)} ${countryName(displayCountry, i18n.language)}`
                : autoDetect ? t('smartMode.autoDetecting') : t('smartMode.selectCountry')}
            </Typography>
            <SearchIcon fontSize="small" sx={{ color: 'text.secondary' }} />
          </ButtonBase>
          <SearchPickerDialog
            open={pickerOpen}
            title={t('smartMode.countryLabel')}
            searchLabel={t('smartMode.searchCountry')}
            emptyText={t('smartMode.noCountryMatch')}
            value={displayCountry || null}
            getOptions={getCountryOptions}
            onClose={() => setPickerOpen(false)}
            onSelect={handleCountryPick}
            optionTestIdPrefix="country-option-"
          />

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
