/**
 * ModeChip — one-line status chip under the Connect button.
 *
 * Surfaces the active "smart mode" resolution so the user can tell at a
 * glance why the Connect button is about to route them through a particular
 * profile. Four display states:
 *
 *   auto + detectedCountry  →  "Detected: 🇷🇺 Russia · Smart bypass · Change"
 *   auto + no detection     →  "Smart mode (detecting...) · Change"
 *   global                  →  "Global mode · Change"
 *   manual (chnroute)       →  "China bypass (manual) · Change"
 *   manual (global)         →  "Global (manual) · Change"
 *
 * Clicking "Change" opens a `ModeSelector` menu anchored to the trigger.
 * The chip itself is a thin presentational component — all state comes from
 * `useConfigStore`.
 */

import { useState, useCallback, type MouseEvent } from 'react';
import { Box, Button, Stack, Typography, useTheme } from '@mui/material';
import { useTranslation } from 'react-i18next';

import { useConfigStore } from '../stores/config.store';
import { countryFlagEmoji, countryName } from '../utils/countries';
import ModeSelector from './ModeSelector';

export interface ModeChipProps {
  /** Optional override for test rendering without touching the store. */
  'data-testid'?: string;
}

export default function ModeChip({ 'data-testid': testId = 'mode-chip' }: ModeChipProps) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();

  const modeOverride = useConfigStore((s) => s.modeOverride);
  const detectedCountry = useConfigStore((s) => s.detectedCountry);
  const ruleMode = useConfigStore((s) => s.ruleMode);

  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  const handleOpen = useCallback((e: MouseEvent<HTMLElement>) => {
    setAnchorEl(e.currentTarget);
  }, []);

  const handleClose = useCallback(() => {
    setAnchorEl(null);
  }, []);

  const changeLabel = t('dashboard:smartMode.change', 'Change');

  let content: React.ReactNode;

  if (modeOverride === 'auto') {
    if (detectedCountry) {
      const flag = countryFlagEmoji(detectedCountry);
      const name = countryName(detectedCountry, i18n.language);
      content = (
        <>
          <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
            {t('dashboard:smartMode.detectedLabel', 'Detected:')}
          </Typography>
          <Typography component="span" variant="caption" sx={{ fontSize: '0.85rem' }}>
            {flag}
          </Typography>
          <Typography component="span" variant="caption" fontWeight={600} sx={{ fontSize: '0.75rem' }}>
            {name}
          </Typography>
          <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
            ·
          </Typography>
          <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
            {t('dashboard:smartMode.smartBypass', 'Smart bypass')}
          </Typography>
        </>
      );
    } else {
      content = (
        <>
          <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
            {t('dashboard:smartMode.autoDetecting', 'Smart mode (detecting...)')}
          </Typography>
        </>
      );
    }
  } else if (modeOverride === 'global') {
    content = (
      <>
        <Typography component="span" variant="caption" sx={{ fontSize: '0.85rem' }}>
          🌐
        </Typography>
        <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
          {t('dashboard:smartMode.globalMode', 'Global mode')}
        </Typography>
      </>
    );
  } else {
    // manual
    const isChnroute = ruleMode === 'chnroute';
    content = (
      <>
        <Typography component="span" variant="caption" sx={{ fontSize: '0.85rem' }}>
          {isChnroute ? '🇨🇳' : '🌐'}
        </Typography>
        <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
          {isChnroute
            ? t('dashboard:smartMode.manualChnroute', 'China bypass (manual)')
            : t('dashboard:smartMode.manualGlobal', 'Global (manual)')}
        </Typography>
      </>
    );
  }

  return (
    <Box
      data-testid={testId}
      sx={{
        display: 'flex',
        justifyContent: 'center',
        py: 0.5,
      }}
    >
      <Stack
        direction="row"
        spacing={0.75}
        alignItems="center"
        sx={{
          px: 1.25,
          py: 0.5,
          borderRadius: 999,
          bgcolor: theme.palette.action.hover,
          maxWidth: '90%',
        }}
      >
        {content}
        <Typography component="span" variant="caption" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
          ·
        </Typography>
        <Button
          size="small"
          variant="text"
          onClick={handleOpen}
          data-testid={`${testId}-change`}
          sx={{
            minWidth: 0,
            px: 0.75,
            py: 0,
            fontSize: '0.75rem',
            textTransform: 'none',
            lineHeight: 1.2,
          }}
        >
          {changeLabel}
        </Button>
      </Stack>
      <ModeSelector anchorEl={anchorEl} onClose={handleClose} />
    </Box>
  );
}
