/**
 * ModeSelector — dropdown that lets the user override the smart-mode
 * resolution.
 *
 * Three options:
 *
 *   Smart   → `modeOverride = 'auto'` (default, uses Center `suggestedProfile`)
 *   Global  → `modeOverride = 'global'` (force everything through the tunnel)
 *   Manual  → `modeOverride = 'manual'` (keeps the legacy ruleMode toggle)
 *
 * The Manual option only renders for users who've already picked a
 * ruleMode at some point — i.e. they had a persisted legacy config before
 * the auto-profile feature landed. For fresh installs we hide it so the
 * dropdown only surfaces the two new paths.
 *
 * Anchored as a `Menu` so it opens directly under the trigger button the
 * chip provides. Closing the menu is handled by the parent.
 */

import { Menu, MenuItem, Stack, Typography } from '@mui/material';
import { Check as CheckIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

import { useConfigStore, type ModeOverride } from '../stores/config.store';

export interface ModeSelectorProps {
  anchorEl: HTMLElement | null;
  onClose: () => void;
}

export default function ModeSelector({ anchorEl, onClose }: ModeSelectorProps) {
  const { t } = useTranslation();

  const modeOverride = useConfigStore((s) => s.modeOverride);
  const ruleMode = useConfigStore((s) => s.ruleMode);
  const updateModeOverride = useConfigStore((s) => s.updateModeOverride);

  // Legacy users have a persisted ruleMode before auto-profile landed, but
  // the store doesn't expose the "had legacy" bit directly. Proxy: if the
  // user is currently on manual mode, they obviously qualify; otherwise we
  // hide the Manual option to avoid confusing fresh installs.
  const showManual = modeOverride === 'manual';

  const handleSelect = (mode: ModeOverride) => async () => {
    await updateModeOverride(mode);
    onClose();
  };

  const open = Boolean(anchorEl);

  return (
    <Menu
      anchorEl={anchorEl}
      open={open}
      onClose={onClose}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      transformOrigin={{ vertical: 'top', horizontal: 'center' }}
      slotProps={{
        paper: {
          sx: { minWidth: 260, maxWidth: '90vw' },
        },
      }}
      data-testid="mode-selector-menu"
    >
      <MenuItem onClick={handleSelect('auto')} data-testid="mode-option-auto">
        <Stack direction="row" spacing={1} alignItems="flex-start" sx={{ width: '100%' }}>
          <CheckIcon
            fontSize="small"
            sx={{
              mt: 0.25,
              visibility: modeOverride === 'auto' ? 'visible' : 'hidden',
            }}
          />
          <Stack spacing={0.25} sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="body2" fontWeight={600}>
              {t('dashboard:smartMode.optionSmart', 'Smart')}
            </Typography>
            <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'normal' }}>
              {t('dashboard:smartMode.optionSmartDesc', 'Recommended based on your location')}
            </Typography>
          </Stack>
        </Stack>
      </MenuItem>

      <MenuItem onClick={handleSelect('global')} data-testid="mode-option-global">
        <Stack direction="row" spacing={1} alignItems="flex-start" sx={{ width: '100%' }}>
          <CheckIcon
            fontSize="small"
            sx={{
              mt: 0.25,
              visibility: modeOverride === 'global' ? 'visible' : 'hidden',
            }}
          />
          <Stack spacing={0.25} sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="body2" fontWeight={600}>
              {t('dashboard:smartMode.optionGlobal', 'Global')}
            </Typography>
            <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'normal' }}>
              {t('dashboard:smartMode.optionGlobalDesc', 'All traffic via Overleap')}
            </Typography>
          </Stack>
        </Stack>
      </MenuItem>

      {showManual && (
        <MenuItem onClick={handleSelect('manual')} data-testid="mode-option-manual">
          <Stack direction="row" spacing={1} alignItems="flex-start" sx={{ width: '100%' }}>
            <CheckIcon
              fontSize="small"
              sx={{
                mt: 0.25,
                visibility: modeOverride === 'manual' ? 'visible' : 'hidden',
              }}
            />
            <Stack spacing={0.25} sx={{ flex: 1, minWidth: 0 }}>
              <Typography variant="body2" fontWeight={600}>
                {t('dashboard:smartMode.optionManual', 'Manual')}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'normal' }}>
                {t(
                  'dashboard:smartMode.optionManualDesc',
                  ruleMode === 'global' ? 'Use my saved settings (Global)' : 'Use my saved settings (China bypass)',
                )}
              </Typography>
            </Stack>
          </Stack>
        </MenuItem>
      )}
    </Menu>
  );
}
