import { useCallback, useEffect, useState, useMemo, useRef } from "react";
import {
  Box,
  Typography,
  Stack,
  Button,
  Tooltip,
  styled,
  Collapse,
} from "@mui/material";
import {
  Dns as DnsIcon,
  ExpandMore as ExpandMoreIcon,
  Settings as SettingsIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useNavigate, useLocation } from "react-router-dom";
import { useVPNStatus, useAuthStore } from "../stores";
import { useUser } from "../hooks/useUser";

import { useLoginDialogStore } from "../stores/login-dialog.store";
import { useConfigStore } from '../stores/config.store';
import { EmptyState } from '../components/LoadingAndEmpty';
import { getCurrentAppConfig } from '../config/apps';
import { CollapsibleConnectionSection } from '../components/CollapsibleConnectionSection';
import { useDashboard } from '../stores/dashboard.store';
import { CloudTunnelList } from '../components/CloudTunnelList';
import { authService } from '../services/auth-service';
import type { Tunnel } from '../services/api-types';

// Styled Components for Modern Design
const DashboardContainer = styled(Box)(({ theme }) => ({
  width: "100%",
  height: "100%",
  display: "flex",
  flexDirection: "column",
  overflow: "hidden",
  position: "relative",
  backgroundColor: theme.palette.mode === 'dark'
    ? theme.palette.grey[900]
    : theme.palette.grey[50],
}));

export default function Dashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();

  // Auth state
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const openLoginDialog = useLoginDialogStore((s) => s.open);
  const { user } = useUser();

  // VPN status - managed globally by VPNStatusContext
  const {
    serviceState,
    isDisconnected,
    isError,
    isServiceRunning,
    isRetrying,
    networkAvailable,
    setOptimisticState,
    error,
    serviceConnected,
    isServiceFailedLongTime,
    serviceFailureDuration
  } = useVPNStatus();

  // Get app-specific configuration
  const appConfig = getCurrentAppConfig();
  const proxyRuleConfig = appConfig.features.proxyRule || { visible: true, defaultValue: 'lightweight' };

  // VPN config from persistent store
  const { ruleMode, updateConfig, buildConnectConfig } = useConfigStore();

  // Service failure alert tracking (silent mode - no UI feedback)
  const [failureAlertSent, setFailureAlertSent] = useState(false);

  // Use dashboard store for persistent state
  const {
    advancedSettingsExpanded: showAdvancedSettings,
    toggleAdvancedSettings: toggleShowAdvancedSettings,
    scrollPosition,
    setScrollPosition,
  } = useDashboard();

  // Ref for scroll container
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Restore scroll position on mount
  useEffect(() => {
    if (scrollContainerRef.current && scrollPosition > 0) {
      scrollContainerRef.current.scrollTop = scrollPosition;
    }
  }, []);

  // Save scroll position on unmount
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    const handleScroll = () => {
      setScrollPosition(container.scrollTop);
    };

    container.addEventListener('scroll', handleScroll, { passive: true });

    return () => {
      container.removeEventListener('scroll', handleScroll);
    };
  }, [setScrollPosition]);

  // Workaround: WebKit compositing bug — force repaint when tab becomes visible
  // after being hidden by keep-alive system, to ensure opacity/filter layer changes
  // are properly recomposited
  const containerRef = useRef<HTMLDivElement>(null);
  const wasHidden = useRef(false);

  useEffect(() => {
    const isVisible = location.pathname === '/';
    if (!isVisible) {
      wasHidden.current = true;
      return;
    }
    if (wasHidden.current && containerRef.current) {
      wasHidden.current = false;
      const el = containerRef.current;
      el.style.transform = 'translateZ(0)';
      requestAnimationFrame(() => {
        el.style.transform = '';
      });
    }
  }, [location.pathname]);

  // Get proxy rule types
  const proxyRules = useMemo(() => {
    return [
      {
        type: 'global',
        label: t('dashboard:dashboard.rule.global'),
        icon: '🌍',
        description: t('dashboard:dashboard.ruleDescription.global')
      },
      {
        type: 'chnroute',
        label: t('dashboard:dashboard.rule.chnroute'),
        icon: '⚡',
        description: t('dashboard:dashboard.ruleDescription.chnroute')
      }
    ];
  }, [t]);

  // For CollapsibleConnectionSection - track selected cloud tunnel
  const [selectedCloudTunnel, setSelectedCloudTunnel] = useState<Tunnel | null>(null);

  // Active tunnel info derived from selected cloud tunnel
  const activeTunnelInfo = useMemo(() => {
    if (!selectedCloudTunnel) {
      return { domain: '', name: '', country: '' };
    }
    return {
      domain: selectedCloudTunnel.domain.toLowerCase(),
      name: selectedCloudTunnel.name || selectedCloudTunnel.domain,
      country: selectedCloudTunnel.node?.country || '',
    };
  }, [selectedCloudTunnel]);

  // Handle cloud tunnel selection (UI state only, no config persistence)
  const handleCloudTunnelSelect = useCallback(async (tunnel: Tunnel, _echConfigList?: string) => {
    if (isServiceRunning) return;
    console.debug('[Dashboard] Selecting cloud tunnel:', tunnel.domain);
    setSelectedCloudTunnel(tunnel);
  }, [isServiceRunning]);

  // Handle rule type selection via config store
  const handleRuleTypeChange = useCallback((ruleType: string) => {
    updateConfig({ rule: { global: ruleType === 'global' } });
  }, [updateConfig]);

  // Effect to detect prolonged failure and trigger silent alert
  useEffect(() => {
    if (isServiceFailedLongTime && !failureAlertSent) {
      handleServiceFailureAlert();
    }

    if (serviceConnected && failureAlertSent) {
      console.info('[Dashboard] Service recovered, resetting alert state');
      setFailureAlertSent(false);
    }
  }, [isServiceFailedLongTime, serviceConnected, failureAlertSent]);

  // Handle service failure alert
  const handleServiceFailureAlert = useCallback(async () => {
    setFailureAlertSent(true);

    if (!window._platform?.uploadLogs) {
      console.debug('[Dashboard] Log upload skipped - platform does not support uploadLogs');
      return;
    }

    try {
      console.debug('[Dashboard] Silently uploading service logs');
      await window._platform.uploadLogs!({
        email: isAuthenticated ? user?.loginIdentifies?.[0]?.value : null,
        reason: 'service_connection_timeout',
        failureDurationMs: serviceFailureDuration ?? undefined,
        platform: window._platform.os,
        version: window._platform.version,
      });

      console.debug('[Dashboard] Service failure reported (silent)');
    } catch (error) {
      console.debug('[Dashboard] Log upload skipped:', error);
    }
  }, [isAuthenticated, serviceFailureDuration]);

  // Resolve server URL: inject auth for k2v5 protocol
  const resolveServerUrl = useCallback(async (serverUrl?: string): Promise<string | undefined> => {
    if (!serverUrl?.startsWith('k2v5://')) return serverUrl;
    return authService.buildTunnelUrl(serverUrl);
  }, []);

  // Handle connection toggle
  const handleToggleConnection = useCallback(async () => {
    if ((isDisconnected || isError) && !activeTunnelInfo.domain) {
      console.warn('[Dashboard] No tunnel selected');
      return;
    }

    try {
      if (isError && !isRetrying) {
        // Error state: reconnect
        console.info('[Dashboard] Reconnecting VPN after error...');
        setOptimisticState('connecting');
        const serverUrl = await resolveServerUrl(selectedCloudTunnel?.serverUrl);
        const config = buildConnectConfig(serverUrl);
        await window._k2.run('up', config);
        updateConfig({ server: selectedCloudTunnel?.serverUrl });
      } else if (!isDisconnected || isRetrying) {
        // Connected/connecting/retrying: disconnect
        console.info('[Dashboard] Stopping VPN...');
        setOptimisticState('disconnecting');
        await window._k2.run('down');
      } else {
        // Disconnected: connect
        console.info('[Dashboard] Starting VPN...');
        setOptimisticState('connecting');
        const serverUrl = await resolveServerUrl(selectedCloudTunnel?.serverUrl);
        const config = buildConnectConfig(serverUrl);
        await window._k2.run('up', config);
        updateConfig({ server: selectedCloudTunnel?.serverUrl });
      }
    } catch (err) {
      console.error('Connection operation failed', err);
      setOptimisticState(null);
    }
  }, [isDisconnected, isError, isRetrying, activeTunnelInfo.domain, selectedCloudTunnel, buildConnectConfig, updateConfig, setOptimisticState, resolveServerUrl]);

  // Check if any tunnel is selected
  const hasTunnelSelected = !!activeTunnelInfo.domain;

  return (
    <DashboardContainer
      ref={containerRef}
      sx={{
        ...(isServiceFailedLongTime && {
          pointerEvents: 'none',
          opacity: 0.5,
          filter: 'grayscale(30%)',
        }),
      }}
    >
      {/* SECTION 1: Connection Control */}
      <CollapsibleConnectionSection
        serviceState={serviceState}
        hasTunnelSelected={hasTunnelSelected}
        tunnelName={activeTunnelInfo.name}
        tunnelCountry={activeTunnelInfo.country}
        onToggle={handleToggleConnection}
        error={error}
        isRetrying={isRetrying}
        networkAvailable={networkAvailable}
      />

      {/* SECTION 2: Tunnel Lists */}
      <Box
        ref={scrollContainerRef}
        sx={{
          flex: 1,
          overflowY: 'auto',
          overflowX: 'hidden',
          '&::-webkit-scrollbar': { width: '4px' },
          '&::-webkit-scrollbar-track': { background: 'transparent' },
          '&::-webkit-scrollbar-thumb': {
            background: (theme) => theme.palette.mode === 'dark'
              ? 'rgba(255, 255, 255, 0.2)'
              : 'rgba(0, 0, 0, 0.2)',
            borderRadius: '4px',
          },
          scrollbarWidth: 'thin',
        }}
      >
        {/* Cloud Tunnels - Only for authenticated users */}
        {isAuthenticated && (
          <Box sx={{ mt: 2 }}>
            <CloudTunnelList
              selectedDomain={activeTunnelInfo.domain}
              onSelect={handleCloudTunnelSelect}
              disabled={isServiceRunning}
            />
          </Box>
        )}

        {/* Empty state for unauthenticated users */}
        {!isAuthenticated && (
          <Box sx={{ px: 2, py: 4 }}>
            <EmptyState
              icon={<DnsIcon sx={{ fontSize: 48 }} />}
              title={t('dashboard:dashboard.guestEmpty.title') || 'Login to get nodes'}
              description={t('dashboard:dashboard.guestEmpty.description') || 'Login to access cloud nodes for stable service.'}
              action={
                <Stack direction="column" spacing={2} sx={{ mt: 2, alignItems: 'center' }}>
                  <Button
                    onClick={() => openLoginDialog({ trigger: 'dashboard-empty' })}
                    variant="contained"
                    color="primary"
                    size="large"
                    sx={{
                      px: 6,
                      py: 1.5,
                      fontSize: '1rem',
                      fontWeight: 600,
                      borderRadius: 2,
                    }}
                  >
                    {t('dashboard:dashboard.loginToGet') || 'Login'}
                  </Button>
                  <Button
                    onClick={() => navigate('/tunnels')}
                    color="inherit"
                    variant="text"
                    size="small"
                    sx={{
                      color: 'text.secondary',
                      textTransform: 'none',
                      fontSize: '0.75rem',
                    }}
                  >
                    {t('dashboard:dashboard.selfDeploy') || 'Self-deploy'}
                  </Button>
                </Stack>
              }
              minHeight={200}
            />
          </Box>
        )}
      </Box>


      {/* SECTION 3: Advanced Settings */}
      <Box sx={{
        borderTop: (theme) => `1px solid ${theme.palette.divider}`,
        backgroundColor: 'background.paper',
        mt: 'auto',
      }}>
        <Button
          fullWidth
          variant="text"
          onClick={toggleShowAdvancedSettings}
          endIcon={<ExpandMoreIcon sx={{
            transform: showAdvancedSettings ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.3s ease',
          }} />}
          startIcon={<SettingsIcon />}
          sx={{ py: 1, px: 2, justifyContent: 'space-between', textTransform: 'none' }}
        >
          <Typography variant="body2" fontWeight={600}>
            {t('dashboard:dashboard.advancedSettings') || 'Advanced Settings'}
          </Typography>
        </Button>

        <Collapse in={showAdvancedSettings}>
          <Box sx={{
            maxHeight: '40vh',
            overflowY: 'auto',
            px: 2,
            pb: 0.5,
            pt: 1,
          }}>
            {isServiceRunning && (
              <Typography variant="caption" sx={{
                color: 'warning.main',
                fontSize: '0.75rem',
                fontWeight: 500,
                mb: 2,
                display: 'block'
              }}>
                {t('dashboard:dashboard.disconnectToModify')}
              </Typography>
            )}

            {/* Proxy Rules Section */}
            {proxyRuleConfig.visible && (
              <Box sx={{ mb: 2.5 }}>
                <Typography variant="body2" fontWeight={600} sx={{ mb: 1.5 }}>
                  {t('dashboard:dashboard.proxyRules')}
                </Typography>
                <Stack direction="row" spacing={0.5} sx={{ width: '100%' }}>
                  {proxyRules.map((rule) => {
                    const isActive = ruleMode === rule.type;
                    return (
                      <Tooltip key={`proxy-rule-${rule.type}`} title={rule.description} arrow>
                        <span style={{ flex: 1, display: 'flex' }}>
                          <Button
                            onClick={() => !isServiceRunning && handleRuleTypeChange(rule.type)}
                            disabled={isServiceRunning}
                            variant={isActive ? "contained" : "outlined"}
                            sx={{
                              flex: 1,
                              minWidth: 0,
                              fontSize: '0.75rem',
                              textTransform: 'none',
                              '&.Mui-disabled': { opacity: 0.6 },
                            }}
                            startIcon={<Box component="span">{rule.icon}</Box>}
                          >
                            {rule.label}
                          </Button>
                        </span>
                      </Tooltip>
                    );
                  })}
                </Stack>
              </Box>
            )}

          </Box>
        </Collapse>
      </Box>

    </DashboardContainer>
  );
}