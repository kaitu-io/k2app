/**
 * K2subConfig — gateway-mode subscription tab content.
 *
 * Auto row (daemon picks best node) + one row per country derived from `tunnels`.
 * Selection is managed by the connection.store (`subsCountry`); this component
 * is a controlled view that calls `setSubsCountry(code | null)`.
 */
import { useMemo } from 'react';
import {
  Box, List, ListItem, ListItemIcon, ListItemText, Radio,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useTranslation } from 'react-i18next';
import { SmartModeIcon } from './SmartModeIcon';
import { getFlagIcon, getCountryName } from '../utils/country';
import { buildCountryList } from '../utils/country-list';
import type { Tunnel } from '../services/api-types';

interface Props {
  tunnels: Tunnel[];
  subsCountry: string | null;
  setSubsCountry: (c: string | null) => void;
  isInteractive: boolean;
}

export function K2subConfig({ tunnels, subsCountry, setSubsCountry, isInteractive }: Props) {
  const { t } = useTranslation('dashboard');
  const theme = useTheme();
  const countries = useMemo(() => buildCountryList(tunnels).map(e => e.code), [tunnels]);

  const selectedBg = theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)';
  const rowSx = (selected: boolean) => ({
    borderRadius: 2,
    minHeight: 56,
    cursor: isInteractive ? 'pointer' : 'default',
    bgcolor: selected ? selectedBg : undefined,
    transition: 'background 0.15s',
    '&:hover': isInteractive ? { bgcolor: selected ? selectedBg : 'action.hover' } : {},
  });

  const handleSelect = (code: string | null) => {
    if (!isInteractive) return;
    setSubsCountry(code);
  };

  return (
    <Box>
      <List disablePadding sx={{ px: 2 }}>
        <ListItem
          disableGutters
          onClick={() => handleSelect(null)}
          sx={rowSx(subsCountry === null)}
        >
          <ListItemIcon sx={{ minWidth: 40 }}><SmartModeIcon /></ListItemIcon>
          <ListItemText
            primary={t('serverSelector.countryAuto')}
            secondary={t('serverSelector.smartHint')}
            primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
            secondaryTypographyProps={{ fontSize: '0.72rem' }}
          />
          <Radio
            checked={subsCountry === null}
            color="primary"
            size="small"
            sx={{ '& .MuiSvgIcon-root': { fontSize: 22 } }}
          />
        </ListItem>

        {countries.map(code => (
          <ListItem
            key={code}
            disableGutters
            onClick={() => handleSelect(code)}
            sx={rowSx(subsCountry === code)}
          >
            <ListItemIcon sx={{ minWidth: 40 }}>{getFlagIcon(code)}</ListItemIcon>
            <ListItemText
              primary={getCountryName(code)}
              primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
            />
            <Radio
              checked={subsCountry === code}
              color="primary"
              size="small"
              sx={{ '& .MuiSvgIcon-root': { fontSize: 22 } }}
            />
          </ListItem>
        ))}
      </List>
    </Box>
  );
}
