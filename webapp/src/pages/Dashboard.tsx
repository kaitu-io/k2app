import { useCallback, useEffect, useState, useMemo, useRef } from "react";
import {
  Box,
  Typography,
  Switch,
  FormControlLabel,
  Stack,
  Button,
  Tooltip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Chip,
  styled,
  Collapse,
  ToggleButton,
  ToggleButtonGroup,
} from "@mui/material";
import {
  Dns as DnsIcon,
  Info as InfoIcon,
  ExpandMore as ExpandMoreIcon,
  Settings as SettingsIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useVPNStatus, useAuthStore } from "../stores";
import { useUser } from "../hooks/useUser";

import { useLoginDialogStore } from "../stores/login-dialog.store";
import type { ConfigResponseData } from "../services/control-types";
import { EmptyState } from '../components/LoadingAndEmpty';
import { getCurrentAppConfig } from '../config/apps';
import { HighlightedText } from '../components/HighlightedText';
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

  // Auth state
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const openLoginDialog = useLoginDialogStore((s) => s.open);
  const { user } = useUser();

  // VPN status - managed globally by VPNStatusContext
  const {
    serviceState,
    isDisconnected,
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

  // Local config state - fetch from control directly
  const [config, setConfig] = useState<ConfigResponseData | null>(null);

  // Load config on mount
  useEffect(() => {
    const loadConfig = async () => {
      try {
        const response = await window._k2.run('get_config');
        if (response.code === 0 && response.data) {
          setConfig(response.data as any);
        }
      } catch (error) {
        console.error('Failed to load config:', error);
      }
    };
    loadConfig();
  }, []);

  // Helper to get proxy rule
  const getProxyRule = (cfg: ConfigResponseData | null): string => {
    if (!cfg) return proxyRuleConfig.defaultValue;
    return cfg.rule?.type || proxyRuleConfig.defaultValue;
  };

  const [activeRuleType, setActiveRuleType] = useState<string>(
    getProxyRule(config)
  );
  const [showAdvancedOptionsHelp, setShowAdvancedOptionsHelp] = useState(false);

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

  // Initialize active rule type from config
  useEffect(() => {
    const rule = getProxyRule(config);
    if (rule) {
      setActiveRuleType(rule);
    }
  }, [config]);

  // Parse active tunnel info from config - SINGLE SOURCE OF TRUTH
  // Uses tunnel.items[0] when mode=items
  const activeTunnelInfo = useMemo(() => {
    const tunnelUrl = config?.tunnel?.mode === 'cloud' && config?.tunnel?.items?.[0]
      ? config.tunnel.items[0]
      : '';

    if (!tunnelUrl) {
      return { domain: '', name: '', anonymity: false, country: '' };
    }

    // Parse tunnel URL: k2v4://domain?ipv4=ip&port=port&country=XX#name
    try {
      // Handle k2v4:// protocol
      const normalized = tunnelUrl.replace(/^k2v4:\/\//, 'https://');
      const parsed = new URL(normalized);
      const country = parsed.searchParams.get('country') || '';
      const name = parsed.hash ? decodeURIComponent(parsed.hash.slice(1)) : parsed.hostname;
      return {
        domain: parsed.hostname.toLowerCase(),
        name,
        anonymity: parsed.searchParams.get('anonymity') === '1',
        country,
      };
    } catch {
      return { domain: '', name: '', anonymity: false, country: '' };
    }
  }, [config?.tunnel]);

  // For CollapsibleConnectionSection - track selected cloud tunnel
  const [selectedCloudTunnel, setSelectedCloudTunnel] = useState<Tunnel | null>(null);

  // Sync selectedCloudTunnel when cloud tunnels are loaded
  // This restores the selection state after app restart
  const handleCloudTunnelsLoaded = useCallback((tunnels: Tunnel[]) => {
    if (!activeTunnelInfo.domain || selectedCloudTunnel) return;

    // Find the tunnel matching the persisted domain
    const matchingTunnel = tunnels.find(
      t => t.domain.toLowerCase() === activeTunnelInfo.domain
    );
    if (matchingTunnel) {
      setSelectedCloudTunnel(matchingTunnel);
    }
  }, [activeTunnelInfo.domain, selectedCloudTunnel]);

  // Local wrapper method for updating configuration
  // Only sends fields that service actually processes
  const handleConfigUpdate = useCallback(
    async (patch: Partial<ConfigResponseData>) => {
      if (!config) return;

      const mergedConfig = {
        ...config,
        ...patch,
      };

      // Only send fields that service actually handles (snake_case)
      const newConfig: Partial<ConfigResponseData> = {
        mode: mergedConfig.mode,
        socks5_addr: mergedConfig.socks5_addr,
        tunnel: mergedConfig.tunnel,
        rule: mergedConfig.rule,
        k2v4: mergedConfig.k2v4,
        ipv6: mergedConfig.ipv6,
        dns_mode: mergedConfig.dns_mode,
        insecure: mergedConfig.insecure,
      };

      try {
        console.debug('[Dashboard] Sending set_config: ' + JSON.stringify(newConfig));
        const response = await window._k2.run('set_config', newConfig);
        if (response.code === 0 && response.data) {
          setConfig(response.data);
          console.info('[Dashboard] Config updated successfully');
        } else {
          console.error('[Dashboard] Failed to update config:', response.code, response.message);
        }
      } catch (error) {
        console.error('[Dashboard] Failed to update config:', error);
      }
    },
    [config]
  );

  // Handle cloud tunnel selection
  // Rust service expects tunnels as k2v4:// URL array
  const handleCloudTunnelSelect = useCallback(async (tunnel: Tunnel, echConfigList?: string) => {
    if (isServiceRunning) return;

    // Build k2v4:// URL format for Rust service with country and name
    const name = tunnel.name || tunnel.domain;
    const country = tunnel.node?.country;

    // Build URL with separate ipv4 and port parameters (canonical format)
    // NOTE: sni is NOT needed - backend automatically uses domain for TLS SNI
    let tunnelUrl = `k2v4://${tunnel.domain}?ipv4=${tunnel.node.ipv4}`;
    if (tunnel.port && tunnel.port !== 443) {
      tunnelUrl += `&port=${tunnel.port}`;
    }
    // Include ECH config list for K2v4 connections (enables encrypted SNI)
    if (echConfigList) {
      tunnelUrl += `&ech_config=${encodeURIComponent(echConfigList)}`;
    }
    if (country) {
      tunnelUrl += `&country=${encodeURIComponent(country)}`;
    }
    tunnelUrl += `#${encodeURIComponent(name)}`;

    console.debug('[Dashboard] Selecting cloud tunnel: ' + JSON.stringify({ domain: tunnel.domain, url: tunnelUrl }));

    // Update selected cloud tunnel state for UI
    setSelectedCloudTunnel(tunnel);

    // Send bare URL - service reads credentials from k2v4 config at connection time
    await handleConfigUpdate({
      tunnel: {
        mode: 'cloud',
        items: [tunnelUrl],
      },
    });
  }, [isServiceRunning, handleConfigUpdate]);

  // Handle rule type selection
  const handleRuleTypeChange = useCallback(async (ruleType: string) => {
    try {
      // Use new rule structure (with backward compatibility via backend auto-migration)
      await handleConfigUpdate({
        rule: {
          type: ruleType,
          antiporn: config?.rule?.antiporn || false,
        },
      });
    } catch (error) {
      console.error('Failed to set rule type:', error);
    }
  }, [handleConfigUpdate, config?.rule?.antiporn]);

  // Handle Anonymity toggle
  // TODO: Anonymity feature not yet supported in Rust service k2v4:// format
  const handleAnonymityToggle = useCallback(async (_e: any) => {
    console.warn('[Dashboard] Anonymity toggle not yet supported in Rust service');
    // Anonymity requires server-side support in k2v4 protocol
  }, []);

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

    if (!window._platform?.uploadServiceLogs) {
      console.debug('[Dashboard] Log upload skipped - platform does not support uploadServiceLogs');
      return;
    }

    try {
      console.debug('[Dashboard] Silently uploading service logs');
      await window._platform.uploadServiceLogs!({
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

  // Handle connection toggle
  const handleToggleConnection = useCallback(async () => {
    if (isDisconnected && !activeTunnelInfo.domain) {
      console.warn('[Dashboard] No tunnel selected');
      return;
    }

    try {
      if (!isDisconnected || isRetrying) {
        console.info('[Dashboard] Stopping VPN...');
        setOptimisticState('disconnecting');
        await window._k2.run('stop');
      } else {
        console.info('[Dashboard] Starting VPN...');
        setOptimisticState('connecting');
        await window._k2.run('start');
      }
    } catch (err) {
      console.error('Connection operation failed', err);
      setOptimisticState(null);
    }
  }, [isDisconnected, isRetrying, activeTunnelInfo.domain, setOptimisticState]);

  // Check if any tunnel is selected (for Anonymity toggle)
  const hasTunnelSelected = !!activeTunnelInfo.domain;

  return (
    <DashboardContainer
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
        tunnelCountry={activeTunnelInfo.country || selectedCloudTunnel?.node?.country}
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
              onTunnelsLoaded={handleCloudTunnelsLoaded}
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
                    const isActive = activeRuleType === rule.type;
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

            {/* K2 Advanced Options Section */}
            <Box>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 1.5 }}>
                <Typography variant="body2" fontWeight={600}>
                  {t('dashboard:dashboard.k2Options')}
                </Typography>
                <Tooltip title={t('dashboard:dashboard.advancedOptionsInfo')}>
                  <InfoIcon sx={{ fontSize: 16, color: 'text.secondary' }} />
                </Tooltip>
              </Box>
              <Stack direction="column" spacing={1.5}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={activeTunnelInfo.anonymity}
                      onChange={handleAnonymityToggle}
                      disabled={isServiceRunning || !hasTunnelSelected}
                    />
                  }
                  label={
                    <Box>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <Typography variant="body2">{t('dashboard:dashboard.enableAnonymity')}</Typography>
                        <Chip label={t('common:common.experimental')} size="small" color="warning" sx={{ height: 18, fontSize: '0.65rem' }} />
                      </Box>
                      <Typography variant="caption" color="text.secondary" component="span">
                        <HighlightedText text={t('dashboard:dashboard.anonymityDescription')} />
                      </Typography>
                    </Box>
                  }
                  sx={{ m: 0, alignItems: 'flex-start' }}
                />
                {/* DNS Mode Selection */}
                <Box>
                  <Typography variant="body2" sx={{ mb: 0.5 }}>{t('dashboard:dashboard.dnsMode')}</Typography>
                  <Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 1 }}>
                    {t('dashboard:dashboard.dnsModeDescription')}
                  </Typography>
                  <ToggleButtonGroup
                    value={config?.dns_mode || 'fake-ip'}
                    exclusive
                    onChange={(_e, value) => value && handleConfigUpdate({ dns_mode: value })}
                    disabled={isServiceRunning}
                    size="small"
                    fullWidth
                    sx={{ '& .MuiToggleButton-root': { flex: 1, textTransform: 'none', fontSize: '0.75rem' } }}
                  >
                    <ToggleButton value="fake-ip">
                      {t('dashboard:dashboard.dnsOptions.fakeIp')}
                    </ToggleButton>
                    <ToggleButton value="real-ip">
                      {t('dashboard:dashboard.dnsOptions.realIp')}
                    </ToggleButton>
                  </ToggleButtonGroup>
                </Box>
              </Stack>
            </Box>
          </Box>
        </Collapse>
      </Box>

      {/* Advanced Options Help Dialog */}
      <Dialog
        open={showAdvancedOptionsHelp}
        onClose={() => setShowAdvancedOptionsHelp(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('dashboard:dashboard.advancedOptionsHelp')}</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5} sx={{ mt: 1 }}>
            <Box>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
                <Typography variant="subtitle2" fontWeight={600} sx={{ color: 'warning.main' }}>
                  {t('dashboard:dashboard.enableAnonymity')}
                </Typography>
                <Chip label={t('common:common.experimental')} size="small" color="warning" sx={{ height: 18, fontSize: '0.65rem' }} />
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1.8 }}>
                <HighlightedText text={t('dashboard:dashboard.anonymityDescription')} />
              </Typography>
            </Box>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowAdvancedOptionsHelp(false)}>
            {t('common:common.ok')}
          </Button>
        </DialogActions>
      </Dialog>
    </DashboardContainer>
  );
}