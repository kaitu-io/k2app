/**
 * CountryFilterDialog — Auto-pick country exclusion filter.
 *
 * Controlled component: exclusion state lives in connection.store
 * (`excludedCountries`); toggles apply immediately (no confirm-commit
 * semantics). "清除" unchecks everything; "完成" only closes.
 * Manual selection is intentionally unaffected by this filter.
 */
import { useMemo } from 'react';
import {
  Dialog, DialogTitle, DialogActions, Typography, List, ListItemButton,
  ListItemIcon, ListItemText, Checkbox, Button,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { getFlagIcon, getCountryName } from '../utils/country';
import { buildCountryList } from '../utils/country-list';
import type { Tunnel } from '../services/api-types';

interface CountryFilterDialogProps {
  open: boolean;
  onClose: () => void;
  tunnels: Tunnel[];
  /** lowercase ISO 3166-1 alpha-2 codes currently excluded */
  excludedCountries: string[];
  onToggle: (code: string) => void;
  onClear: () => void;
}

export function CountryFilterDialog({
  open, onClose, tunnels, excludedCountries, onToggle, onClear,
}: CountryFilterDialogProps) {
  const { t } = useTranslation('dashboard');
  const countries = useMemo(() => buildCountryList(tunnels), [tunnels]);
  const excluded = useMemo(() => new Set(excludedCountries), [excludedCountries]);

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle sx={{ pb: 0.5 }}>{t('auto.filterTitle')}</DialogTitle>
      <Typography variant="body2" color="text.secondary" sx={{ px: 3, pb: 1 }}>
        {t('auto.filterHint')}
      </Typography>
      <List disablePadding>
        {countries.map(({ code, count }) => (
          <ListItemButton key={code} onClick={() => onToggle(code)} sx={{ px: 3, minHeight: 48 }}>
            <ListItemIcon sx={{ minWidth: 36, fontSize: 20 }}>{getFlagIcon(code)}</ListItemIcon>
            <ListItemText
              primary={getCountryName(code)}
              primaryTypographyProps={{ fontSize: '0.88rem', fontWeight: 500 }}
            />
            <Typography variant="caption" color="text.secondary" sx={{ mr: 1.5 }}>
              {t('auto.nodeCount', { count })}
            </Typography>
            <Checkbox edge="end" checked={excluded.has(code)} tabIndex={-1} disableRipple />
          </ListItemButton>
        ))}
      </List>
      <DialogActions>
        <Button
          data-testid="country-filter-clear"
          color="inherit"
          onClick={onClear}
          disabled={excludedCountries.length === 0}
        >
          {t('auto.filterClear')}
        </Button>
        <Button data-testid="country-filter-done" onClick={onClose}>
          {t('auto.filterDone')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
