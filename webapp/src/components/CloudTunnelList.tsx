import { useCallback, useEffect, useState, useMemo, useRef } from 'react';
import {
  Box,
  Typography,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Radio,
  Stack,
  IconButton,
  Tooltip,
  Button,
  useTheme,
} from '@mui/material';
import { Refresh as RefreshIcon, CloudOff as CloudOffIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { getCountryName, getFlagIcon } from '../utils/country';
import { getThemeColors } from '../theme/colors';
import { LoadingState, EmptyState } from './LoadingAndEmpty';
import { VerticalLoadBar } from './VerticalLoadBar';
import { k2api } from '../services/k2api';
import { sortTunnelsByRecommendation } from '../utils/tunnel-sort';
import { useAuthStore } from '../stores/auth.store';
import { useVPNStore } from '../stores/vpn.store';
import type { Tunnel, TunnelListResponse } from '../services/api-types';

interface CloudTunnelListProps {
  selectedDomain: string | null;
  onSelect: (tunnel: Tunnel, echConfigList?: string) => void;
  disabled?: boolean;
  onTunnelsLoaded?: (tunnels: Tunnel[]) => void;
}

export function CloudTunnelList({ selectedDomain, onSelect, disabled, onTunnelsLoaded }: CloudTunnelListProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

  const [tunnels, setTunnels] = useState<Tunnel[]>([]);
  const [echConfigList, setEchConfigList] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Stable ref for callback to avoid re-triggering effects
  const onTunnelsLoadedRef = useRef(onTunnelsLoaded);
  onTunnelsLoadedRef.current = onTunnelsLoaded;

  // Subscribe to auth and service connection states for auto-refresh
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const serviceConnected = useVPNStore((s) => s.serviceConnected);

  // Track previous values to detect state transitions
  const prevAuthRef = useRef(isAuthenticated);
  const prevServiceConnectedRef = useRef(serviceConnected);

  // Sort tunnels by recommendation (neutral quality - no evaluation)
  const neutralQualityProvider = useMemo(() => ({ getRouteQuality: () => 0 }), []);
  const sortedTunnels = useMemo(() => {
    return sortTunnelsByRecommendation(tunnels, neutralQualityProvider);
  }, [tunnels, neutralQualityProvider]);

  // Retry state for automatic retry on error
  const retryCountRef = useRef(0);
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const refresh = useCallback(async () => {
    // Clear any pending retry timeout
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current);
      retryTimeoutRef.current = null;
    }

    try {
      setRefreshing(true);
      setError(null);
      const response = await k2api({
        cache: { key: 'api:tunnels', ttl: 10, allowExpired: true }
      }).exec<TunnelListResponse>('api_request', {
        method: 'GET',
        path: '/api/tunnels/k2v4'
      });

      if (response.code === 0 && response.data) {
        const loadedTunnels = response.data.items || [];
        setTunnels(loadedTunnels);
        // Store ECH config list for K2v4 connections
        setEchConfigList(response.data.echConfigList);
        console.debug('[CloudTunnelList] ECH config from API:', response.data.echConfigList ? `present (len=${response.data.echConfigList.length})` : 'empty');
        // Reset retry count on success
        retryCountRef.current = 0;
        // Notify parent about loaded tunnels for state sync
        onTunnelsLoadedRef.current?.(loadedTunnels);
      } else if (response.code !== 0) {
        // Handle non-success response codes (e.g., 401, -1 for network error)
        // Skip logging for 401 since user is just not logged in
        if (response.code !== 401) {
          console.error('Failed to fetch cloud tunnels, code:', response.code, response.message);
        }
        setError(new Error(response.message || 'Failed to load cloud tunnels'));
        // Schedule auto-retry with exponential backoff (max 5 retries, 3s/6s/12s/24s/48s)
        if (retryCountRef.current < 5) {
          const delay = Math.min(3000 * Math.pow(2, retryCountRef.current), 48000);
          retryCountRef.current += 1;
          console.log(`[CloudTunnelList] Scheduling retry #${retryCountRef.current} in ${delay}ms`);
          retryTimeoutRef.current = setTimeout(() => {
            refresh();
          }, delay);
        }
      }
    } catch (err) {
      console.error('Failed to fetch cloud tunnels:', err);
      setError(err instanceof Error ? err : new Error('Unknown error'));
    } finally {
      setRefreshing(false);
      setLoading(false);
    }
  }, []);

  // Initial load and cleanup
  useEffect(() => {
    refresh();
    // Cleanup on unmount
    return () => {
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current);
      }
    };
  }, [refresh]);

  // Auto-refresh every 5 minutes
  useEffect(() => {
    const interval = setInterval(refresh, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, [refresh]);

  // Refresh when authentication state changes (login success)
  useEffect(() => {
    const wasAuthenticated = prevAuthRef.current;
    prevAuthRef.current = isAuthenticated;

    // Refresh when user logs in (false -> true)
    if (!wasAuthenticated && isAuthenticated) {
      console.info('[CloudTunnelList] Auth state changed to authenticated, refreshing tunnels');
      refresh();
    }
  }, [isAuthenticated, refresh]);

  // Refresh when service connection recovers
  useEffect(() => {
    const wasConnected = prevServiceConnectedRef.current;
    prevServiceConnectedRef.current = serviceConnected;

    // Refresh when service connection recovers (false -> true)
    if (!wasConnected && serviceConnected) {
      console.info('[CloudTunnelList] Service connection recovered, refreshing tunnels');
      refresh();
    }
  }, [serviceConnected, refresh]);

  if (loading && tunnels.length === 0) {
    return (
      <Box sx={{ px: 2, py: 1 }}>
        <LoadingState message={t('dashboard:dashboard.loadingNodes')} minHeight={100} />
      </Box>
    );
  }

  // Show error state with retry button when load failed and no tunnels
  if (error && tunnels.length === 0) {
    return (
      <Box sx={{ px: 2, py: 2 }}>
        <Stack spacing={1.5} alignItems="center">
          <Typography variant="body2" color="error.main" textAlign="center">
            {t('dashboard:dashboard.errorLoadingNodes')}
          </Typography>
          <IconButton
            onClick={refresh}
            disabled={refreshing}
            sx={{
              bgcolor: 'action.hover',
              '&:hover': { bgcolor: 'action.selected' },
            }}
          >
            <RefreshIcon
              sx={{
                fontSize: 24,
                animation: refreshing ? 'spin 1s linear infinite' : 'none',
                '@keyframes spin': {
                  '0%': { transform: 'rotate(0deg)' },
                  '100%': { transform: 'rotate(360deg)' }
                }
              }}
            />
          </IconButton>
          <Typography variant="caption" color="text.secondary">
            {t('dashboard:dashboard.retryLoading')}
          </Typography>
        </Stack>
      </Box>
    );
  }

  // Show empty state when no tunnels available (not due to error)
  if (tunnels.length === 0 && !error) {
    return (
      <Box sx={{ px: 2, py: 2 }}>
        <EmptyState
          icon={<CloudOffIcon sx={{ fontSize: 48, color: 'text.disabled' }} />}
          title={t('dashboard:dashboard.noNodesTitle')}
          description={t('dashboard:dashboard.noNodesDescription')}
          action={
            <Button
              onClick={refresh}
              disabled={refreshing}
              variant="outlined"
              size="small"
              startIcon={
                <RefreshIcon
                  sx={{
                    fontSize: 18,
                    animation: refreshing ? 'spin 1s linear infinite' : 'none',
                    '@keyframes spin': {
                      '0%': { transform: 'rotate(0deg)' },
                      '100%': { transform: 'rotate(360deg)' }
                    }
                  }}
                />
              }
            >
              {t('dashboard:dashboard.refreshNodes')}
            </Button>
          }
          minHeight={150}
        />
      </Box>
    );
  }

  return (
    <Box>
      {/* Sticky Header */}
      <Stack
        direction="row"
        spacing={1}
        sx={{
          py: 1,
          px: 2,
          alignItems: 'center',
          justifyContent: 'space-between',
          position: 'sticky',
          top: 0,
          bgcolor: 'background.default',
          zIndex: 1,
          borderBottom: `1px solid ${theme.palette.divider}`,
        }}
      >
        <Stack direction="row" spacing={1} alignItems="center">
          <Typography variant="overline" fontWeight={600} color="text.secondary" sx={{ fontSize: '0.7rem' }}>
            ☁️ {t('dashboard:dashboard.cloudNodes') || 'Cloud Nodes'}
          </Typography>
        </Stack>
        <Stack direction="row" spacing={0.5} alignItems="center">
          {error && (
            <Typography variant="caption" sx={{ color: 'error.main', fontSize: '0.65rem' }}>
              {t('dashboard:dashboard.refreshFailed')}
            </Typography>
          )}
          <Tooltip title={t('dashboard:dashboard.manualRefresh') || 'Refresh'}>
            <IconButton size="small" onClick={refresh} disabled={refreshing} sx={{ p: 0.5 }}>
              <RefreshIcon
                sx={{
                  fontSize: 18,
                  animation: refreshing ? 'spin 1s linear infinite' : 'none',
                  '@keyframes spin': {
                    '0%': { transform: 'rotate(0deg)' },
                    '100%': { transform: 'rotate(360deg)' }
                  }
                }}
              />
            </IconButton>
          </Tooltip>
        </Stack>
      </Stack>

      {/* List */}
      <List sx={{ pt: 0.5, px: 2, pb: 1 }}>
        {sortedTunnels.map((tunnel) => {
          const domain = tunnel.domain.toLowerCase();
          const isSelected = selectedDomain === domain;
          return (
            <ListItem
              key={tunnel.id}
              onClick={() => !disabled && onSelect(tunnel, echConfigList)}
              sx={{
                borderRadius: 2,
                mb: 0.5,
                minHeight: 64,
                bgcolor: isSelected ? colors.selectedBg : undefined,
                cursor: disabled ? 'not-allowed' : 'pointer',
                opacity: disabled ? 0.6 : 1,
                transition: 'all 0.2s ease',
                '&:hover': {
                  bgcolor: disabled ? undefined : 'action.hover',
                  transform: disabled ? 'none' : 'scale(1.01)',
                },
              }}
            >
              <ListItemIcon sx={{ minWidth: 40, fontSize: 24 }}>
                {getFlagIcon(tunnel.node.country)}
              </ListItemIcon>
              <ListItemText
                primary={
                  <Box component="span" sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    {tunnel.name || tunnel.domain}
                  </Box>
                }
                secondary={getCountryName(tunnel.node.country)}
                primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
                secondaryTypographyProps={{ fontSize: '0.75rem' }}
              />

              {/* Vertical load bar (de-emphasized) */}
              <Box sx={{ mr: 2 }}>
                <VerticalLoadBar load={tunnel.node.load} />
              </Box>

              <Radio
                checked={isSelected}
                color="primary"
                value={domain}
                sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }}
              />
            </ListItem>
          );
        })}
      </List>
    </Box>
  );
}
