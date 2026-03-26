import { useCallback, useEffect, useState, useMemo, useRef } from "react";
import {
  Box,
  Typography,
  Stack,
  Button,
  Tooltip,
  styled,
  Collapse,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Radio,
  IconButton,
  useTheme,
} from "@mui/material";
import {
  ExpandMore as ExpandMoreIcon,
  Settings as SettingsIcon,
  Terminal as TerminalIcon,
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
import { getCurrentAppConfig } from '../config/apps';
import { CollapsibleConnectionSection } from '../components/CollapsibleConnectionSection';
import { useDashboard } from '../stores/dashboard.store';
import { CloudTunnelList } from '../components/CloudTunnelList';
import { getThemeColors } from '../theme/colors';
import { getCountryName, getFlagIcon } from '../utils/country';
import type { Tunnel, TunnelListResponse } from '../services/api-types';
import { cacheStore } from '../services/cache-store';
import { DisconnectFeedbackDialog } from '../components/DisconnectFeedbackDialog';

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
    enrichFromTunnelList,
  } = useConnectionStore();

  // Display tunnel: snapshot during connection, selection otherwise
  const displayTunnel = connectedTunnel ?? activeTunnel;

  // Cold start / warm start enrichment: when connectedTunnel has domain but no country,
  // try to enrich from cached tunnel list immediately (covers warm start where
  // CloudTunnelList already loaded and won't re-fire onTunnelsLoaded)
  useEffect(() => {
    if (connectedTunnel?.source === 'cloud' && !connectedTunnel.country) {
      const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
      if (cached?.items) {
        enrichFromTunnelList(cached.items);
      }
    }
  }, [connectedTunnel, enrichFromTunnelList]);

  // Get app-specific configuration
  const appConfig = getCurrentAppConfig();
  const proxyRuleConfig = appConfig.features.proxyRule || { visible: true, defaultValue: 'lightweight' };

  // VPN config from persistent store
  const { ruleMode, updateConfig } = useConfigStore();

  // Theme colors
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const colors = getThemeColors(isDark);

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
    console.debug('[Dashboard] handleCloudTunnelSelect: tunnel=' + _tunnel.domain + ', isInteractive=' + isInteractive);
    if (isInteractive) {
      console.warn('[Dashboard] handleCloudTunnelSelect: blocked by isInteractive (vpnState=' + vpnState + ')');
      return;
    }
    selectCloudTunnel(_tunnel);
  }, [isInteractive, vpnState, selectCloudTunnel]);

  // Handle self-hosted tunnel selection
  const handleSelfHostedSelect = useCallback(() => {
    if (isInteractive) {
      console.warn('[Dashboard] handleSelfHostedSelect: blocked by isInteractive (vpnState=' + vpnState + ')');
      return;
    }
    selectSelfHosted();
  }, [isInteractive, vpnState, selectSelfHosted]);

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
    console.debug('[Dashboard] handleToggleConnection: vpnState=' + vpnState + ', isConnected=' + isConnected + ', isDisconnected=' + isDisconnected + ', isTransitioning=' + isTransitioning + ', isRetrying=' + isRetrying + ', hasTunnel=' + !!displayTunnel);
    if (vpnState === 'disconnecting') {
      console.debug('[Dashboard] handleToggleConnection: already disconnecting, ignoring');
      return;
    }
    if (isConnected || isTransitioning) {
      console.info('[Dashboard] handleToggleConnection: → disconnect');
      disconnect();
    } else if (isDisconnected) {
      if (!displayTunnel) {
        console.warn('[Dashboard] handleToggleConnection: no tunnel selected, aborting');
        return;
      }
      console.info('[Dashboard] handleToggleConnection: → connect (tunnel=' + displayTunnel.domain + ')');
      connect();
    } else {
      console.warn('[Dashboard] handleToggleConnection: no matching branch (vpnState=' + vpnState + ', isRetrying=' + isRetrying + ')');
    }
  }, [isConnected, isDisconnected, isTransitioning, vpnState, displayTunnel, connect, disconnect]);

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
          display: 'flex',
          flexDirection: 'column',
          minHeight: 0,
          // Authenticated: this box scrolls all tunnel lists
          // Unauthenticated: no scroll here, phantom area handles its own scroll
          ...(!isAuthenticated ? {} : {
            overflowY: 'auto',
            overflowX: 'hidden',
          }),
        }}
      >
        {/* Cloud Tunnels - Only for authenticated users */}
        {isAuthenticated && (
          <Box sx={{ mt: 2 }}>
            <CloudTunnelList
              selectedDomain={displayTunnel?.domain || ''}
              onSelect={handleCloudTunnelSelect}
              disabled={isInteractive}
              onTunnelsLoaded={enrichFromTunnelList}
            />
          </Box>
        )}

        {/* Self-hosted node — shown below cloud list for authenticated, or as primary for guests */}
        {selfHostedTunnel && (
          <Box sx={{ flexShrink: 0 }}>
            {/* Section header — only for authenticated users (guests see it as primary) */}
            {isAuthenticated && (
              <Stack
                direction="row"
                spacing={1}
                sx={{
                  py: 1,
                  px: 2,
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  borderBottom: `1px solid ${theme.palette.divider}`,
                }}
              >
                <Typography variant="overline" fontWeight={600} color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                  <TerminalIcon sx={{ fontSize: 14, mr: 0.5, verticalAlign: 'text-bottom' }} />
                  {t('dashboard:selfHosted.tag')}
                </Typography>
                <Tooltip title={t('dashboard:selfHosted.manageNode')}>
                  <IconButton size="small" onClick={() => navigate('/tunnels')} sx={{ p: 0.5 }}>
                    <SettingsIcon sx={{ fontSize: 16 }} />
                  </IconButton>
                </Tooltip>
              </Stack>
            )}

            <List sx={{ pt: 0.5, px: 2, pb: 1 }}>
              <ListItem
                onClick={handleSelfHostedSelect}
                sx={{
                  borderRadius: 2,
                  minHeight: 64,
                  bgcolor: selectedSource === 'self_hosted' ? colors.selectedBg : undefined,
                  cursor: isInteractive ? 'not-allowed' : 'pointer',
                  opacity: isInteractive ? '0.6 !important' : 1,
                  transition: 'all 0.2s ease',
                  '&:hover': {
                    bgcolor: isInteractive ? undefined : 'action.hover',
                    transform: isInteractive ? 'none' : 'scale(1.01)',
                  },
                }}
              >
                <ListItemIcon sx={{ minWidth: 40 }}>
                  {selfHostedTunnel.country ? (
                    getFlagIcon(selfHostedTunnel.country)
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
                      {selfHostedTunnel.name}
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
                        {t('dashboard:dashboard.selfDeployed')}
                      </Typography>
                    </Box>
                  }
                  secondary={selfHostedTunnel.country ? getCountryName(selfHostedTunnel.country) : t('dashboard:selfHosted.tag')}
                  primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
                  secondaryTypographyProps={{ fontSize: '0.75rem' }}
                />
                <IconButton
                  size="small"
                  onClick={(e) => {
                    e.stopPropagation();
                    navigate('/tunnels');
                  }}
                  sx={{ mr: 0.5 }}
                >
                  <SettingsIcon sx={{ fontSize: 18, color: 'text.secondary' }} />
                </IconButton>
                <Radio
                  checked={selectedSource === 'self_hosted'}
                  color="primary"
                  sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }}
                />
              </ListItem>
            </List>
          </Box>
        )}

        {/* Phantom cloud nodes for unauthenticated users */}
        {!isAuthenticated && (
          <Box sx={{ position: 'relative', flex: 1, minHeight: 0, overflow: 'hidden' }}>
            {/* Scrollable blurred list — scrolls independently */}
            <List sx={{
              height: '100%',
              overflowY: 'auto',
              pt: 0.5,
              px: 2,
              pb: 1,
              filter: 'blur(4px)',
              opacity: 0.5,
              pointerEvents: 'none',
              userSelect: 'none',
            }}>
              {[
                { flag: 'JP', name: 'Tokyo-01', country: 'Japan' },
                { flag: 'JP', name: 'Tokyo-02', country: 'Japan' },
                { flag: 'SG', name: 'Singapore-01', country: 'Singapore' },
                { flag: 'SG', name: 'Singapore-02', country: 'Singapore' },
                { flag: 'US', name: 'Los Angeles-01', country: 'United States' },
                { flag: 'US', name: 'San Jose-01', country: 'United States' },
                { flag: 'HK', name: 'Hong Kong-01', country: 'Hong Kong' },
                { flag: 'HK', name: 'Hong Kong-02', country: 'Hong Kong' },
                { flag: 'TW', name: 'Taipei-01', country: 'Taiwan' },
                { flag: 'KR', name: 'Seoul-01', country: 'South Korea' },
                { flag: 'DE', name: 'Frankfurt-01', country: 'Germany' },
                { flag: 'GB', name: 'London-01', country: 'United Kingdom' },
                { flag: 'AU', name: 'Sydney-01', country: 'Australia' },
                { flag: 'CA', name: 'Toronto-01', country: 'Canada' },
                { flag: 'FR', name: 'Paris-01', country: 'France' },
              ].map((item) => (
                <ListItem
                  key={item.name}
                  sx={{
                    borderRadius: 2,
                    mb: 0.5,
                    minHeight: 64,
                    bgcolor: 'action.hover',
                  }}
                >
                  <ListItemIcon sx={{ minWidth: 40, fontSize: 24 }}>
                    {getFlagIcon(item.flag)}
                  </ListItemIcon>
                  <ListItemText
                    primary={item.name}
                    secondary={item.country}
                    primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
                    secondaryTypographyProps={{ fontSize: '0.75rem' }}
                  />
                  <Radio disabled color="primary" sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }} />
                </ListItem>
              ))}
            </List>

            {/* Overlay — fixed in visible area, not affected by list scroll */}
            <Box sx={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              bgcolor: `${theme.palette.background.default}99`,
              pointerEvents: 'none',
            }}>
              <Stack spacing={1.5} alignItems="center" sx={{ pointerEvents: 'auto' }}>
                <Button
                  variant="contained"
                  color="primary"
                  onClick={() => openLoginDialog({ trigger: 'dashboard-upgrade' })}
                  sx={{ fontWeight: 600, px: 3 }}
                >
                  {t('dashboard:dashboard.unlockCloudNodes')}
                </Button>
                <Button
                  variant="text"
                  size="small"
                  onClick={() => navigate('/tunnels')}
                  sx={{
                    color: 'text.secondary',
                    textTransform: 'none',
                    fontSize: '0.75rem',
                  }}
                >
                  {t('dashboard:dashboard.selfDeploy')}
                </Button>
              </Stack>
            </Box>
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

      <DisconnectFeedbackDialog />
    </DashboardContainer>
  );
}
