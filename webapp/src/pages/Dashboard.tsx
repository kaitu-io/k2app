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
import { useAuthStore } from "../stores";
import { useUser } from "../hooks/useUser";

import { useLoginDialogStore } from "../stores/login-dialog.store";
import { useConfigStore } from '../stores/config.store';
import { useConnectionStore } from '../stores/connection.store';
import { useVPNMachine } from '../stores/vpn-machine.store';
import { useSelfHostedStore } from '../stores/self-hosted.store';
import { EmptyState } from '../components/LoadingAndEmpty';
import { getCurrentAppConfig } from '../config/apps';
import { CollapsibleConnectionSection } from '../components/CollapsibleConnectionSection';
import { useDashboard } from '../stores/dashboard.store';
import { CloudTunnelList } from '../components/CloudTunnelList';
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

  // VPN state machine
  const {
    state: vpnState,
    isDisconnected,
    isConnected,
    isServiceDown,
    isTransitioning,
    isInteractive,
    isRetrying,
    networkAvailable,
    error,
  } = useVPNMachine();

  // Connection orchestration
  const {
    selectedSource,
    activeTunnel,
    connectedTunnel,
    selectCloudTunnel,
    selectSelfHosted,
    connect,
    disconnect,
  } = useConnectionStore();

  // Display tunnel: snapshot during connection, selection otherwise
  const displayTunnel = connectedTunnel ?? activeTunnel;

  // Get app-specific configuration
  const appConfig = getCurrentAppConfig();
  const proxyRuleConfig = appConfig.features.proxyRule || { visible: true, defaultValue: 'lightweight' };

  // VPN config from persistent store
  const { ruleMode, updateConfig } = useConfigStore();

  // Self-hosted tunnel
  const selfHostedTunnel = useSelfHostedStore((s) => s.tunnel);

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

  // Handle cloud tunnel selection
  const handleCloudTunnelSelect = useCallback((_tunnel: Tunnel, _echConfigList?: string) => {
    if (isInteractive) return;
    selectCloudTunnel(_tunnel);
  }, [isInteractive, selectCloudTunnel]);

  // Handle self-hosted tunnel selection
  const handleSelfHostedSelect = useCallback(() => {
    if (isInteractive) return;
    selectSelfHosted();
  }, [isInteractive, selectSelfHosted]);

  // Handle rule type selection via config store
  const handleRuleTypeChange = useCallback((ruleType: string) => {
    updateConfig({ rule: { global: ruleType === 'global' } });
  }, [updateConfig]);

  // Effect to detect prolonged failure and trigger silent alert
  useEffect(() => {
    if (isServiceDown && !failureAlertSent) {
      handleServiceFailureAlert();
    }

    if (!isServiceDown && failureAlertSent) {
      console.info('[Dashboard] Service recovered, resetting alert state');
      setFailureAlertSent(false);
    }
  }, [isServiceDown, failureAlertSent]);

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
        platform: window._platform.os,
        version: window._platform.version,
      });

      console.debug('[Dashboard] Service failure reported (silent)');
    } catch (error) {
      console.debug('[Dashboard] Log upload skipped:', error);
    }
  }, [isAuthenticated]);

  // Connection toggle
  const handleToggleConnection = useCallback(async () => {
    if (isConnected || isTransitioning || (vpnState === 'error' && isRetrying)) {
      disconnect();
    } else if (isDisconnected || (vpnState === 'error' && !isRetrying)) {
      if (!displayTunnel) {
        console.warn('[Dashboard] No tunnel selected');
        return;
      }
      connect();
    }
  }, [isConnected, isDisconnected, isTransitioning, vpnState, isRetrying, displayTunnel, connect, disconnect]);

  // Check if any tunnel is selected (cloud or self-hosted)
  const hasTunnelSelected = !!displayTunnel;

  // Map vpnState to ServiceState for CollapsibleConnectionSection
  const serviceState = vpnState === 'idle' ? 'disconnected'
    : vpnState === 'serviceDown' ? 'disconnected'
    : vpnState;

  // Auto-select self-hosted when it's the only option (guest with tunnel)
  useEffect(() => {
    if (!isAuthenticated && selfHostedTunnel && selectedSource === 'cloud' && !activeTunnel) {
      selectSelfHosted();
    }
  }, [isAuthenticated, selfHostedTunnel, selectedSource, activeTunnel, selectSelfHosted]);

  return (
    <DashboardContainer
      ref={containerRef}
      sx={{
        ...(isServiceDown && {
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
        tunnelName={displayTunnel?.name}
        tunnelCountry={displayTunnel?.country}
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
              selectedDomain={displayTunnel?.domain || ''}
              onSelect={handleCloudTunnelSelect}
              disabled={isInteractive}
            />
          </Box>
        )}

        {/* Self-hosted node — shown below cloud list for authenticated, or as primary for guests */}
        {selfHostedTunnel && (
          <Box sx={{ px: 2, py: 1 }}>
            <Button
              fullWidth
              variant={selectedSource === 'self_hosted' ? 'contained' : 'outlined'}
              onClick={handleSelfHostedSelect}
              disabled={isInteractive}
              sx={{
                justifyContent: 'flex-start',
                textTransform: 'none',
                py: 1.5,
                px: 2,
                borderRadius: 2,
              }}
            >
              <Stack direction="row" spacing={1.5} alignItems="center">
                {selfHostedTunnel.country && (
                  <Typography variant="body2">{selfHostedTunnel.country}</Typography>
                )}
                <Box>
                  <Typography variant="body2" fontWeight={600} textAlign="left">
                    {selfHostedTunnel.name}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" sx={{ opacity: 0.7 }}>
                    {t('dashboard:selfHosted.tag')}
                  </Typography>
                </Box>
              </Stack>
            </Button>
          </Box>
        )}

        {/* Empty state for unauthenticated users without self-hosted tunnel */}
        {!isAuthenticated && !selfHostedTunnel && (
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

        {/* Cloud upgrade CTA for guests with self-hosted tunnel */}
        {!isAuthenticated && selfHostedTunnel && (
          <Box sx={{ px: 2, py: 1 }}>
            <Button
              fullWidth
              variant="text"
              onClick={() => openLoginDialog({ trigger: 'dashboard-upgrade' })}
              sx={{
                textTransform: 'none',
                color: 'text.secondary',
                fontSize: '0.75rem',
              }}
            >
              {t('dashboard:selfHosted.upgradeTitle')} {t('dashboard:selfHosted.upgradeCta')}
            </Button>
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
            {isInteractive && (
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
                            onClick={() => !isInteractive && handleRuleTypeChange(rule.type)}
                            disabled={isInteractive}
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
