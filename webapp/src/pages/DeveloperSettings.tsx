import { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Typography,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListItemSecondaryAction,
  FormControl,
  Select,
  MenuItem,
  Divider,
  Alert,
  Card,
  CardContent,
  IconButton,
  useTheme,
} from '@mui/material';
import {
  Code as CodeIcon,
  BugReport as BugReportIcon,
  ArrowBack as ArrowBackIcon,
  Warning as WarningIcon,
  SettingsEthernet as SettingsEthernetIcon,
  AltRoute as AltRouteIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import type { ConfigResponseData } from '../services/control-types';

// VPN mode options
const VPN_MODES = [
  { value: 'tun', label: 'tun' },
  { value: 'socks5', label: 'socks5' },
  { value: 'tproxy', label: 'tproxy' },
] as const;
type VpnMode = (typeof VPN_MODES)[number]['value'];

// Log levels available for selection
const LOG_LEVELS = ['ERROR', 'WARN', 'INFO', 'DEBUG', 'TRACE'] as const;
type LogLevel = (typeof LOG_LEVELS)[number];

// Path type filter options
// Note: 'all' is used as UI value, transformed to empty string when sending to backend
const PATH_TYPE_FILTERS = [
  { value: 'all', label: 'all' },
  { value: 'quic', label: 'quic' },
  { value: 'tcp-ws', label: 'tcpWs' },
] as const;
type PathTypeFilter = (typeof PATH_TYPE_FILTERS)[number]['value'];

export default function DeveloperSettings() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const theme = useTheme();

  const [vpnMode, setVpnMode] = useState<VpnMode>('tun');
  const [logLevel, setLogLevel] = useState<LogLevel>('INFO');
  const [pathTypeFilter, setPathTypeFilter] = useState<PathTypeFilter>('all');
  const [loading, setLoading] = useState(true);

  // Load all settings from config on mount
  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const configResponse = await window._k2.run<ConfigResponseData>('get_config');

      if (configResponse?.data) {
        const config = configResponse.data;

        // VPN mode
        if (config.mode) {
          setVpnMode(config.mode as VpnMode);
        }

        // Log level from config.log.level
        if (config.log?.level) {
          setLogLevel(config.log.level.toUpperCase() as LogLevel);
        }

        // Path type filter from k2v4 config
        if (config.k2v4) {
          const { tcp_ws, quic_pcc } = config.k2v4;
          if (tcp_ws && quic_pcc) {
            setPathTypeFilter('all');
          } else if (tcp_ws) {
            setPathTypeFilter('tcp-ws');
          } else if (quic_pcc) {
            setPathTypeFilter('quic');
          } else {
            setPathTypeFilter('all');
          }
        }
      }
    } catch (error) {
      console.error('[DeveloperSettings] Failed to load settings:', error);
    } finally {
      setLoading(false);
    }
  }, []);

  // Load settings on mount
  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  const handleVpnModeChange = async (newMode: VpnMode) => {
    try {
      await window._k2.run('set_config', { mode: newMode });
      setVpnMode(newMode);
      console.info(`[DeveloperSettings] VPN mode set to ${newMode}`);
    } catch (error) {
      console.error('[DeveloperSettings] Failed to set VPN mode:', error);
    }
  };

  const handleLogLevelChange = async (newLevel: LogLevel) => {
    try {
      await window._k2.run('set_config', { log: { level: newLevel } });
      setLogLevel(newLevel);
      console.info(`[DeveloperSettings] Log level set to ${newLevel}`);
    } catch (error) {
      console.error('[DeveloperSettings] Failed to set log level:', error);
    }
  };

  const handlePathTypeFilterChange = async (newFilter: PathTypeFilter) => {
    const k2v4 = {
      tcp_ws: newFilter === 'all' || newFilter === 'tcp-ws',
      quic_pcc: newFilter === 'all' || newFilter === 'quic',
    };
    try {
      await window._k2.run('set_config', { k2v4 });
      setPathTypeFilter(newFilter);
      console.info(`[DeveloperSettings] Path type filter set to: ${newFilter} (k2v4: tcp_ws=${k2v4.tcp_ws}, quic_pcc=${k2v4.quic_pcc})`);
    } catch (error) {
      console.error('[DeveloperSettings] Failed to set path type filter:', error);
    }
  };

  return (
    <Box sx={{ width: '100%', py: 0.5, backgroundColor: 'transparent' }}>
      {/* Header */}
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1 }}>
        <IconButton onClick={() => navigate(-1)} size="small">
          <ArrowBackIcon />
        </IconButton>
        <Typography variant="h6" sx={{ fontWeight: 600 }}>
          {t('developer:developer.title', 'Developer Settings')}
        </Typography>
      </Box>

      {/* Warning Banner */}
      <Alert
        severity="warning"
        icon={<WarningIcon />}
        sx={{ mb: 2, borderRadius: 2 }}
      >
        <Typography variant="body2">
          {t('developer:developer.warning', 'These settings are for developers only. Incorrect settings may affect app performance.')}
        </Typography>
      </Alert>

      {/* General Settings */}
      <Typography variant="caption" color="text.secondary" sx={{ px: 1, mb: 0.5, display: 'block', fontWeight: 500 }}>
        {t('developer:developer.sectionGeneral', 'GENERAL')}
      </Typography>
      <Card
        sx={{
          borderRadius: 2,
          background: theme.palette.mode === 'dark'
            ? `linear-gradient(145deg, ${theme.palette.grey[800]} 0%, ${theme.palette.grey[900]} 100%)`
            : `linear-gradient(145deg, ${theme.palette.background.paper} 0%, ${theme.palette.grey[50]} 100%)`,
          boxShadow: 'none',
          border: theme.palette.mode === 'dark'
            ? `1px solid ${theme.palette.grey[700]}`
            : `1px solid ${theme.palette.grey[200]}`,
          mb: 2.5,
        }}
      >
        <CardContent sx={{ p: 0 }}>
          <List>
            {/* VPN Mode Setting */}
            <ListItem sx={{ py: 1.5 }}>
              <ListItemIcon>
                <SettingsEthernetIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('developer:developer.vpnMode', 'VPN Mode')}
                  </Typography>
                }
                secondary={
                  <Typography variant="caption" color="text.secondary">
                    {t('developer:developer.vpnModeDesc', 'Select VPN operation mode')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <FormControl size="small" sx={{ minWidth: 120 }}>
                  <Select
                    value={vpnMode}
                    onChange={(e) => handleVpnModeChange(e.target.value as VpnMode)}
                    disabled={loading}
                    variant="outlined"
                    sx={{
                      borderRadius: 1.5,
                      '& .MuiSelect-select': {
                        py: 1,
                        fontSize: '0.8rem',
                      },
                    }}
                  >
                    {VPN_MODES.map((option) => (
                      <MenuItem key={option.value} value={option.value}>
                        <Typography sx={{ fontSize: '0.8rem' }}>
                          {t(`developer:developer.mode.${option.label}`, option.label.toUpperCase())}
                        </Typography>
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </ListItemSecondaryAction>
            </ListItem>

            <Divider />

            {/* Log Level Setting */}
            <ListItem sx={{ py: 1.5 }}>
              <ListItemIcon>
                <BugReportIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('developer:developer.logLevel', 'Log Level')}
                  </Typography>
                }
                secondary={
                  <Typography variant="caption" color="text.secondary">
                    {t('developer:developer.logLevelDesc', 'Control the verbosity of logging output')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <FormControl size="small" sx={{ minWidth: 120 }}>
                  <Select
                    value={logLevel}
                    onChange={(e) => handleLogLevelChange(e.target.value as LogLevel)}
                    disabled={loading}
                    variant="outlined"
                    sx={{
                      borderRadius: 1.5,
                      '& .MuiSelect-select': {
                        py: 1,
                        fontSize: '0.8rem',
                      },
                    }}
                  >
                    {LOG_LEVELS.map((level) => (
                      <MenuItem key={level} value={level}>
                        <Typography sx={{ fontSize: '0.8rem' }}>{level}</Typography>
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </ListItemSecondaryAction>
            </ListItem>
          </List>
        </CardContent>
      </Card>

      {/* K2V4 Protocol Settings */}
      <Typography variant="caption" color="text.secondary" sx={{ px: 1, mb: 0.5, display: 'block', fontWeight: 500 }}>
        {t('developer:developer.sectionK2v4', 'K2V4 PROTOCOL')}
      </Typography>
      <Card
        sx={{
          borderRadius: 2,
          background: theme.palette.mode === 'dark'
            ? `linear-gradient(145deg, ${theme.palette.grey[800]} 0%, ${theme.palette.grey[900]} 100%)`
            : `linear-gradient(145deg, ${theme.palette.background.paper} 0%, ${theme.palette.grey[50]} 100%)`,
          boxShadow: 'none',
          border: theme.palette.mode === 'dark'
            ? `1px solid ${theme.palette.grey[700]}`
            : `1px solid ${theme.palette.grey[200]}`,
        }}
      >
        <CardContent sx={{ p: 0 }}>
          <List>
            {/* Protocol Filter Setting */}
            <ListItem sx={{ py: 1.5 }}>
              <ListItemIcon>
                <AltRouteIcon />
              </ListItemIcon>
              <ListItemText
                primary={
                  <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                    {t('developer:developer.pathTypeFilter', 'Protocol Filter')}
                  </Typography>
                }
                secondary={
                  <Typography variant="caption" color="text.secondary">
                    {t('developer:developer.pathTypeFilterDesc', 'Enable or disable transport protocols for multipath')}
                  </Typography>
                }
              />
              <ListItemSecondaryAction>
                <FormControl size="small" sx={{ minWidth: 120 }}>
                  <Select
                    value={pathTypeFilter}
                    onChange={(e) => handlePathTypeFilterChange(e.target.value as PathTypeFilter)}
                    disabled={loading}
                    variant="outlined"
                    sx={{
                      borderRadius: 1.5,
                      '& .MuiSelect-select': {
                        py: 1,
                        fontSize: '0.8rem',
                      },
                    }}
                  >
                    {PATH_TYPE_FILTERS.map((option) => (
                      <MenuItem key={option.value} value={option.value}>
                        <Typography sx={{ fontSize: '0.8rem' }}>
                          {t(`developer:developer.filter.${option.label}`, option.label === 'all' ? 'All' : option.label.toUpperCase())}
                        </Typography>
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </ListItemSecondaryAction>
            </ListItem>
          </List>
        </CardContent>
      </Card>

      {/* Info Section */}
      <Box sx={{ mt: 2, px: 1 }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
          <CodeIcon sx={{ fontSize: 14, verticalAlign: 'middle', mr: 0.5 }} />
          {t('developer:developer.devModeInfo', 'Developer mode activated. Settings reset on app restart.')}
        </Typography>
      </Box>
    </Box>
  );
}
