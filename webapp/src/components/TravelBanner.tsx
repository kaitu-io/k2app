/**
 * TravelBanner — dismissible banner prompting the user to confirm a newly
 * detected country when they appear to have travelled.
 *
 * Trigger condition:
 *
 *   modeOverride === 'auto'
 *   && detectedCountry != null
 *   && detectedCountry !== lastAcknowledgedCountry
 *
 * The banner offers two actions, both of which update
 * `lastAcknowledgedCountry` so the banner doesn't re-trigger for the same
 * trip until a new country is detected:
 *
 *   [Switch]   → keep modeOverride=auto, acknowledge, hide banner. The
 *                actual routing switch is implicit — the store already has
 *                suggestedProfile set, so the next connect will use it.
 *   [Dismiss]  → acknowledge only, routing unchanged.
 *
 * Countries outside the 14-profile list show slightly different copy
 * explaining that traffic will route globally.
 */

import { Alert, Box, Button, Stack, Typography } from '@mui/material';
import { FlightTakeoff as PlaneIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useCallback, useMemo } from 'react';

import { useConfigStore } from '../stores/config.store';
import {
  countryFlagEmoji,
  countryName,
  isSupportedCountry,
} from '../utils/countries';

export interface TravelBannerProps {
  'data-testid'?: string;
}

export default function TravelBanner({ 'data-testid': testId = 'travel-banner' }: TravelBannerProps) {
  const { t, i18n } = useTranslation();

  const modeOverride = useConfigStore((s) => s.modeOverride);
  const detectedCountry = useConfigStore((s) => s.detectedCountry);
  const lastAcknowledgedCountry = useConfigStore((s) => s.lastAcknowledgedCountry);
  const updateModeOverride = useConfigStore((s) => s.updateModeOverride);
  const acknowledgeCountry = useConfigStore((s) => s.acknowledgeCountry);

  const shouldShow = useMemo(() => {
    if (modeOverride !== 'auto') return false;
    if (!detectedCountry) return false;
    const detected = detectedCountry.toLowerCase();
    const ack = (lastAcknowledgedCountry ?? '').toLowerCase();
    return detected !== ack;
  }, [modeOverride, detectedCountry, lastAcknowledgedCountry]);

  const handleSwitch = useCallback(async () => {
    // No-op if already auto, but keep the call for symmetry / future-proofing.
    if (modeOverride !== 'auto') {
      await updateModeOverride('auto');
    }
    await acknowledgeCountry(detectedCountry);
  }, [modeOverride, updateModeOverride, acknowledgeCountry, detectedCountry]);

  const handleDismiss = useCallback(async () => {
    await acknowledgeCountry(detectedCountry);
  }, [acknowledgeCountry, detectedCountry]);

  if (!shouldShow || !detectedCountry) return null;

  const flag = countryFlagEmoji(detectedCountry);
  const name = countryName(detectedCountry, i18n.language);
  const supported = isSupportedCountry(detectedCountry);

  return (
    <Box sx={{ px: 2, pt: 1 }} data-testid={testId}>
      <Alert
        severity="info"
        icon={<PlaneIcon fontSize="small" />}
        sx={{
          alignItems: 'center',
          '& .MuiAlert-message': { flex: 1, py: 0.5 },
        }}
        action={
          <Stack direction="row" spacing={0.5}>
            <Button
              size="small"
              variant="contained"
              color="primary"
              onClick={handleSwitch}
              data-testid={`${testId}-switch`}
              sx={{ textTransform: 'none', minWidth: 0, px: 1.5 }}
            >
              {t('dashboard:smartMode.travelSwitch', 'Switch')}
            </Button>
            <Button
              size="small"
              variant="text"
              color="inherit"
              onClick={handleDismiss}
              data-testid={`${testId}-dismiss`}
              sx={{ textTransform: 'none', minWidth: 0, px: 1.5 }}
            >
              {t('dashboard:smartMode.travelDismiss', 'Dismiss')}
            </Button>
          </Stack>
        }
      >
        <Typography variant="body2" sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
          <span>
            {t('dashboard:smartMode.travelPrompt', 'You seem to be in')}
          </span>
          <Box component="span" sx={{ fontSize: '1rem' }}>{flag}</Box>
          <Box component="span" sx={{ fontWeight: 600 }}>{name}</Box>
          <span>
            {supported
              ? t('dashboard:smartMode.travelSwitchPrompt', 'now. Switch to this country\'s profile?')
              : t('dashboard:smartMode.travelGlobalPrompt', 'now. Your traffic will route globally.')}
          </span>
        </Typography>
      </Alert>
    </Box>
  );
}
