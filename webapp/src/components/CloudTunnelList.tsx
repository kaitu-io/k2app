import { forwardRef, useCallback, useEffect, useImperativeHandle, useState, useMemo, useRef } from 'react';
import {
  Box,
  Chip,
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
  Skeleton,
} from '@mui/material';
import { Refresh as RefreshIcon, CloudOff as CloudOffIcon, FlashOn as FlashOnIcon } from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { getCountryName, getFlagIcon } from '../utils/country';
import { getThemeColors } from '../theme/colors';
import { EmptyState } from './LoadingAndEmpty';
import { RecommendBar } from './RecommendBar';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import { useAuthStore } from '../stores/auth.store';
import { useVPNMachineStore } from '../stores/vpn-machine.store';
import type { Tunnel, TunnelListResponse } from '../services/api-types';
import { AUTO_TUNNEL_SENTINEL, AUTO_TUNNEL_DOMAIN, useConnectionStore } from '../stores/connection.store';
import { ERROR_CODES } from '../utils/errorCode';

interface CloudTunnelListProps {
  selectedDomain: string | null;
  onSelect: (tunnel: Tunnel, echConfigList?: string) => void;
  disabled?: boolean;
  onTunnelsLoaded?: (tunnels: Tunnel[]) => void;
  /** Hide the "☁️ 云端节点" sticky header — use when the tab label already provides context. */
  hideHeader?: boolean;
}

/**
 * Imperative handle exposed via ref. Used by Dashboard to wire a manual
 * refresh button that lives outside the list (in SmartServerSelector's
 * tab row).
 */
export interface CloudTunnelListHandle {
  /**
   * Refresh the cloud tunnel list.
   * - `{ force: true }` bypasses the SWR cache-hit fast-path and performs
   *   a blocking fetch, rethrowing on failure so the caller can observe
   *   the outcome.
   * - Default (no opts) uses SWR semantics (cache hit = immediate + background
   *   revalidate), never throws.
   */
  refresh: (opts?: { force?: boolean }) => Promise<void>;
}

export const CloudTunnelList = forwardRef<CloudTunnelListHandle, CloudTunnelListProps>(
function CloudTunnelList({ selectedDomain, onSelect, disabled, onTunnelsLoaded, hideHeader }, ref) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

  // Live entitlement signal: when the tunnel endpoint last returned 402, the
  // cloud list is emptied and this renders the renew prompt. Source of truth
  // is the connection store (also gates the connect button).
  const cloudAccessRevoked = useConnectionStore((s) => s.cloudAccessRevoked);

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
  const serviceConnected = useVPNMachineStore((s) => s.state !== 'serviceDown');

  // Track previous values to detect state transitions
  const prevAuthRef = useRef(isAuthenticated);
  const prevServiceConnectedRef = useRef(serviceConnected);

  // Sort tunnels alphabetically by country code. Probe-backed ranking was
  // removed with the dashboard auto-probe; a future UI can re-introduce a
  // quality-weighted sort by consuming probe-service / probe.store directly.
  const sortedTunnels = useMemo(
    () => [...tunnels].sort((a, b) => a.node.country.localeCompare(b.node.country)),
    [tunnels]
  );

  // Retry state for automatic retry on error
  const retryCountRef = useRef(0);
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // 402 (membership expired) is terminal, not transient: empty the list, cancel
  // any pending backoff, and hand the entitlement truth to the store, which
  // purges the tunnel cache + selection and disables the connect button.
  const applyMembershipRevoked = useCallback(() => {
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current);
      retryTimeoutRef.current = null;
    }
    retryCountRef.current = 0;
    setTunnels([]);
    setEchConfigList(undefined);
    setError(null);
    setLoading(false);
    setRefreshing(false);
    useConnectionStore.getState().setCloudAccess(false);
  }, []);

  const refresh = useCallback(async (opts?: { force?: boolean }): Promise<void> => {
    const force = opts?.force === true;

    // Clear any pending retry timeout — a fresh request supersedes backoff.
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current);
      retryTimeoutRef.current = null;
    }

    setRefreshing(true);
    setError(null);

    // SWR fast-path: return cached immediately, revalidate in background.
    // Skipped when caller explicitly requests a fresh fetch (force=true)
    // so a manual refresh button reflects real network progress.
    if (!force) {
      const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
      if (cached) {
        const loadedTunnels = cached.items || [];
        setTunnels(loadedTunnels);
        setEchConfigList(cached.echConfigList);
        retryCountRef.current = 0;
        onTunnelsLoadedRef.current?.(loadedTunnels);
        // Background revalidate — fire and forget, no state throw.
        cloudApi.get<TunnelListResponse>('/api/v20260717/tunnels').then(res => {
          if (res.code === ERROR_CODES.PAYMENT_REQUIRED) {
            console.warn('[CloudTunnelList] 402 membership expired (background) — clearing cloud tunnels');
            applyMembershipRevoked();
            return;
          }
          if (res.code === 0 && res.data) {
            cacheStore.set('api:tunnels', res.data);
            setTunnels(res.data.items || []);
            setEchConfigList(res.data.echConfigList);
            onTunnelsLoadedRef.current?.(res.data.items || []);
            useConnectionStore.getState().setCloudAccess(true);
          } else {
            console.warn('[CloudTunnelList] Background refresh failed (code=%d), keeping cached tunnels (%d items)', res.code, cached.items?.length ?? 0);
          }
        }).catch(err => {
          console.warn('[CloudTunnelList] Background refresh network error, keeping cached tunnels', err);
        });
        setRefreshing(false);
        setLoading(false);
        return;
      }
    }

    // Blocking fetch — either no cache or force=true.
    try {
      const response = await cloudApi.get<TunnelListResponse>('/api/v20260717/tunnels');

      // 402 = membership expired. Terminal: clear the list + cache + selection
      // and surface the renew prompt. Never retry, never rethrow on force.
      if (response.code === ERROR_CODES.PAYMENT_REQUIRED) {
        console.warn('[CloudTunnelList] 402 membership expired — clearing cloud tunnels');
        applyMembershipRevoked();
        return;
      }

      if (response.code === 0 && response.data) {
        const loadedTunnels = response.data.items || [];
        setTunnels(loadedTunnels);
        setEchConfigList(response.data.echConfigList);
        cacheStore.set('api:tunnels', response.data);
        console.debug('[CloudTunnelList] ECH config from API:', response.data.echConfigList ? `present (len=${response.data.echConfigList.length})` : 'empty');
        retryCountRef.current = 0;
        onTunnelsLoadedRef.current?.(loadedTunnels);
        useConnectionStore.getState().setCloudAccess(true);
      } else {
        // Skip noisy log for 401 (user not logged in yet).
        if (response.code !== 401) {
          console.error('[CloudTunnelList] Failed to fetch cloud tunnels, code:', response.code, response.message);
        }
        setError(new Error('Failed to load cloud tunnels'));
        // Auto-retry with exponential backoff only on non-forced calls;
        // force=true is user-initiated and will surface the failure
        // directly via rethrow, user decides when to retry.
        if (!force && retryCountRef.current < 5) {
          const delay = Math.min(3000 * Math.pow(2, retryCountRef.current), 48000);
          retryCountRef.current += 1;
          console.debug(`[CloudTunnelList] Scheduling retry #${retryCountRef.current} in ${delay}ms`);
          retryTimeoutRef.current = setTimeout(() => {
            void refresh();
          }, delay);
        }
        if (force) {
          throw new Error(`cloud tunnels fetch failed (code=${response.code})`);
        }
      }
    } catch (err) {
      console.error('[CloudTunnelList] Failed to fetch cloud tunnels:', err);
      setError(err instanceof Error ? err : new Error('Unknown error'));
      if (force) throw err;
    } finally {
      setRefreshing(false);
      setLoading(false);
    }
  }, [applyMembershipRevoked]);

  useImperativeHandle(ref, () => ({ refresh }), [refresh]);

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

  // Membership expired (live 402) takes priority over every other list state —
  // the cloud list is empty by definition and the only useful action is renew.
  if (cloudAccessRevoked) {
    return (
      <Box sx={{ px: 2, py: 2 }}>
        <EmptyState
          icon={<CloudOffIcon sx={{ fontSize: 48, color: 'text.disabled' }} />}
          title={t('dashboard:dashboard.membershipExpiredTitle')}
          description={t('dashboard:dashboard.membershipExpiredHint')}
          action={
            // Renew routes to /purchase. On iOS that route only exists when the
            // native StoreKit IAP bridge is present (App.tsx / SideNavigation gate
            // on `_platform.iap`); without it /purchase is unregistered and the
            // CTA would dead-end (Apple 3.1.1). Match that exact gating: show the
            // renew CTA everywhere except iOS-without-IAP. The hint's "switch to
            // Self-hosted" path stays actionable regardless.
            !(window._platform?.os === 'ios' && !window._platform?.iap) ? (
              <Button
                onClick={() => navigate('/purchase')}
                variant="contained"
                size="small"
              >
                {t('dashboard:dashboard.renewMembership')}
              </Button>
            ) : undefined
          }
          minHeight={150}
        />
      </Box>
    );
  }

  if (loading && tunnels.length === 0) {
    return (
      <Box>
        {/* Skeleton header — matches real header structure */}
        <Stack direction="row" sx={{ py: 1, px: 2, alignItems: 'center', justifyContent: 'space-between' }}>
          <Skeleton variant="text" width={100} height={20} />
          <Skeleton variant="circular" width={18} height={18} />
        </Stack>
        {/* Skeleton tunnel items — matches real ListItem structure */}
        <List disablePadding sx={{ px: 2 }}>
          {[0, 1, 2].map((i) => (
            <ListItem key={i} disableGutters sx={{ borderRadius: 2, mb: 0.5, minHeight: 64 }}>
              <ListItemIcon sx={{ minWidth: 40 }}>
                <Skeleton variant="rounded" width={32} height={22} />
              </ListItemIcon>
              <ListItemText
                primary={<Skeleton variant="text" width={`${60 - i * 10}%`} />}
                secondary={<Skeleton variant="text" width={80} />}
              />
              <Box sx={{ mr: 2 }}>
                <Skeleton variant="rounded" width={4} height={28} />
              </Box>
              <Skeleton variant="circular" width={24} height={24} />
            </ListItem>
          ))}
        </List>
      </Box>
    );
  }

  // Show friendly empty state when load failed and no tunnels are
  // cached — a network hiccup shouldn't read as "App is broken",
  // since self-hosted tunnels and future retries remain available.
  if (error && tunnels.length === 0) {
    return (
      <Box sx={{ px: 2, py: 2 }}>
        <EmptyState
          icon={<CloudOffIcon sx={{ fontSize: 48, color: 'text.disabled' }} />}
          title={t('dashboard:dashboard.cloudNodesUnavailable')}
          description={t('dashboard:dashboard.cloudNodesUnavailableHint')}
          action={
            <Button
              onClick={() => { void refresh({ force: true }).catch(() => {}); }}
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
              {t('dashboard:dashboard.retryLoading')}
            </Button>
          }
          minHeight={150}
        />
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
              onClick={() => { void refresh(); }}
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
      {!hideHeader && <Stack
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
          <Tooltip title={t('dashboard:dashboard.manualRefresh') || 'Refresh'}>
            <IconButton size="small" onClick={() => void refresh()} disabled={refreshing} sx={{ p: 0.5 }}>
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
      </Stack>}

      {/* List */}
      <List disablePadding sx={{ px: 2 }}>
        {/* Auto virtual row — first item, ahead of country-sorted tunnels */}
        <ListItem
          key="auto"
          disableGutters
          onClick={() => {
            console.debug('[CloudTunnelList] tunnelClick: AUTO');
            !disabled && onSelect(AUTO_TUNNEL_SENTINEL, echConfigList);
          }}
          sx={{
            borderRadius: 2,
            mb: 0.5,
            minHeight: 64,
            bgcolor: selectedDomain === AUTO_TUNNEL_DOMAIN ? colors.selectedBg : undefined,
            cursor: disabled ? 'not-allowed' : 'pointer',
            opacity: disabled ? '0.6 !important' : 1,
            transition: 'all 0.2s ease',
            '&:hover': {
              bgcolor: disabled ? undefined : 'action.hover',
              transform: disabled ? 'none' : 'scale(1.01)',
            },
          }}
        >
          <ListItemIcon sx={{ minWidth: 40, fontSize: 24 }}>
            <FlashOnIcon sx={{ fontSize: 24, color: 'primary.main' }} />
          </ListItemIcon>
          <ListItemText
            primary={t('dashboard:auto.title')}
            secondary={t('dashboard:auto.subtitle')}
            primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
            secondaryTypographyProps={{ fontSize: '0.75rem' }}
          />
          <Radio
            checked={selectedDomain === AUTO_TUNNEL_DOMAIN}
            color="primary"
            value={AUTO_TUNNEL_DOMAIN}
            sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }}
          />
        </ListItem>

        {sortedTunnels.map((tunnel) => {
          const domain = tunnel.domain.toLowerCase();
          const isSelected = selectedDomain === domain;
          return (
            <ListItem
              key={tunnel.id}
              disableGutters
              onClick={() => {
                console.debug('[CloudTunnelList] tunnelClick: domain=' + tunnel.domain + ', disabled=' + disabled);
                !disabled && onSelect(tunnel, echConfigList);
              }}
              sx={{
                borderRadius: 2,
                mb: 0.5,
                minHeight: 64,
                bgcolor: isSelected ? colors.selectedBg : undefined,
                cursor: disabled ? 'not-allowed' : 'pointer',
                opacity: disabled ? '0.6 !important' : 1,
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
                    {tunnel.ipType === 'residential' && (
                      <Chip
                        label={t('dashboard:tunnels.residentialIp')}
                        size="small"
                        color="success"
                        variant="outlined"
                        sx={{ height: 18, fontSize: '0.65rem', fontWeight: 600 }}
                      />
                    )}
                  </Box>
                }
                secondary={getCountryName(tunnel.node.country)}
                primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
                secondaryTypographyProps={{ fontSize: '0.75rem' }}
              />

              <Box sx={{ mr: 2, display: 'flex', alignItems: 'center' }}>
                <RecommendBar score={tunnel.recommendScore} />
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
);
