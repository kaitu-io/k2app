/**
 * SelfHostedTunnelItem — 自部署隧道 ListItem（认证/未认证共用）
 */

import {
  Box,
  ListItem,
  ListItemIcon,
  ListItemText,
  Radio,
  IconButton,
  Typography,
} from '@mui/material';
import {
  Settings as SettingsIcon,
  Terminal as TerminalIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { getThemeColors } from '../theme/colors';
import { getCountryName, getFlagIcon } from '../utils/country';
import { useTheme } from '@mui/material/styles';
import type { SelfHostedTunnel } from '../stores/self-hosted.store';

interface SelfHostedTunnelItemProps {
  tunnel: SelfHostedTunnel;
  selected: boolean;
  /** Click handler for selecting this tunnel. Omit for non-interactive (e.g. authenticated tab). */
  onSelect?: () => void;
  /** Navigate to tunnel config page. */
  onConfigure: () => void;
  /** Whether the VPN is in a non-interactive state (connecting/connected). */
  disabled?: boolean;
  /** Remove ListItem's default 16px horizontal gutters — use when the parent List provides px:2 padding. */
  disableGutters?: boolean;
}

export function SelfHostedTunnelItem({
  tunnel,
  selected,
  onSelect,
  onConfigure,
  disabled = false,
  disableGutters = false,
}: SelfHostedTunnelItemProps) {
  const { t } = useTranslation('dashboard');
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

  const interactive = !!onSelect && !disabled;

  return (
    <ListItem
      disableGutters={disableGutters}
      onClick={interactive ? onSelect : undefined}
      sx={{
        borderRadius: 2,
        minHeight: 64,
        bgcolor: selected ? colors.selectedBg : undefined,
        cursor: interactive ? 'pointer' : disabled ? 'not-allowed' : 'default',
        opacity: disabled ? '0.6 !important' : 1,
        transition: 'all 0.2s ease',
        '&:hover': interactive
          ? { bgcolor: selected ? colors.selectedBg : 'action.hover', transform: 'scale(1.01)' }
          : {},
      }}
    >
      <ListItemIcon sx={{ minWidth: 40 }}>
        {tunnel.country ? (
          getFlagIcon(tunnel.country)
        ) : (
          <Box sx={{
            width: 32,
            height: 22,
            borderRadius: 0.5,
            bgcolor: colors.accentBgLight,
            border: `1px solid ${colors.accentBorder}`,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}>
            <TerminalIcon sx={{ fontSize: 14, color: colors.accent }} />
          </Box>
        )}
      </ListItemIcon>
      <ListItemText
        primary={
          <Box component="span" sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {tunnel.name}
            <Typography
              component="span"
              sx={{
                fontSize: '0.65rem',
                px: 0.8,
                py: 0.1,
                borderRadius: 0.5,
                bgcolor: colors.accentBgLighter,
                border: `1px solid ${colors.accentBorder}`,
                color: colors.accent,
                fontWeight: 500,
                lineHeight: 1.4,
              }}
            >
              {t('dashboard.selfDeployed')}
            </Typography>
          </Box>
        }
        secondary={tunnel.country ? getCountryName(tunnel.country) : t('selfHosted.tag')}
        primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
        secondaryTypographyProps={{ fontSize: '0.75rem' }}
      />
      <IconButton
        size="small"
        onClick={(e) => {
          e.stopPropagation();
          onConfigure();
        }}
        sx={{ mr: 0.5 }}
      >
        <SettingsIcon sx={{ fontSize: 18, color: 'text.secondary' }} />
      </IconButton>
      <Radio
        checked={selected}
        color="primary"
        sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }}
      />
    </ListItem>
  );
}
