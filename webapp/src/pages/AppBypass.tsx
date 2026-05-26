/**
 * AppBypass — 不走代理的应用 (App Bypass page)
 *
 * Layout (3 sections):
 *  1. Rule card  — wraps the existing <RoutingModeSelector /> + smart-bypass
 *                  status line + manual count + global-mode warning.
 *  2. Manual added — entries the user explicitly added.
 *  3. Add more — search filter + remaining installed apps.
 *
 * Smart bypass for the user's region (Chinese apps, Iran apps, etc.) is now
 * owned by the Go engine via region presets shipped through k2-rules — webapp
 * no longer enumerates installed apps for client-side detection. The
 * ClientConfig.app_bypass.region field at connect time tells the engine
 * which preset to merge with user-added overrides.
 *
 * VPN-guard: the page kicks back to '/' the moment VPN leaves the idle state.
 */
import { useEffect, useCallback, useMemo, useState, useDeferredValue } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box, Typography, Stack, IconButton, CircularProgress, Avatar, Button,
  TextField, Paper, Divider, InputAdornment, LinearProgress,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import SearchIcon from '@mui/icons-material/Search';
import PushPinIcon from '@mui/icons-material/PushPin';
import AddIcon from '@mui/icons-material/Add';
import { useAppBypassStore, useVPNMachineStore, useAlertStore, useConfigStore } from '../stores';
import type { Candidate } from '../stores/app-bypass.store';
import RoutingModeSelector from '../components/RoutingModeSelector';

export default function AppBypass() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const entries = useAppBypassStore((s) => s.entries);
  const candidates = useAppBypassStore((s) => s.candidates);
  const candidatesLoadedAt = useAppBypassStore((s) => s.candidatesLoadedAt);
  const candidatesLoading = useAppBypassStore((s) => s.candidatesLoading);
  const candidatesError = useAppBypassStore((s) => s.candidatesError);
  const refreshCandidates = useAppBypassStore((s) => s.refreshCandidates);
  const featureSupported = useAppBypassStore((s) => s.featureSupported);
  const region = useAppBypassStore((s) => s.region);
  const matched = useAppBypassStore((s) => s.matched);
  const matchedLoading = useAppBypassStore((s) => s.matchedLoading);
  const matchedError = useAppBypassStore((s) => s.matchedError);
  const refreshPreview = useAppBypassStore((s) => s.refreshPreview);
  const daemonBacked = !!window._platform?.appBypass?.daemonBacked;
  const preset = useConfigStore((s) => s.resolvePreset());
  const country = useConfigStore((s) => s.country);
  const smartBypassActive = preset !== 'global' && !!country;
  const [searchQuery, setSearchQuery] = useState('');
  const deferredSearch = useDeferredValue(searchQuery);
  // Frame-flash guard: between initial mount and useEffect firing refreshCandidates,
  // candidatesLoading is still false and candidates is []. Treat "never loaded" as
  // loading so the user never sees an "empty + idle" flash.
  const showInitialLoad = candidatesLoading || candidatesLoadedAt === 0;

  // Page-level VPN guard: redirect to '/' if VPN leaves the idle state.
  useEffect(() => {
    const onState = (state: string) => {
      if (state !== 'idle') {
        navigate('/', { replace: true });
        useAlertStore.getState().showAlert(
          t('dashboard:appBypass.kickedOutDueToConnect'),
          'info',
        );
      }
    };
    const current = useVPNMachineStore.getState().state;
    if (current !== 'idle') {
      onState(current);
      return;
    }
    return useVPNMachineStore.subscribe((s) => s.state, onState);
  }, [navigate, t]);

  // Daemon-platform guard: redirect home if the daemon reports the platform
  // doesn't support per-app attribution (iOS today). `undefined` means "not
  // resolved yet" (mobile / pre-load) so we don't bounce the user prematurely.
  useEffect(() => {
    if (featureSupported === false) {
      navigate('/', { replace: true });
    }
  }, [featureSupported, navigate]);

  // Kick off a background refresh on mount; UI renders cached data immediately.
  useEffect(() => {
    refreshCandidates();
  }, [refreshCandidates]);

  // Refresh the engine-side preview whenever region or daemonBacked changes.
  useEffect(() => {
    if (!daemonBacked || !region) return;
    void refreshPreview();
  }, [daemonBacked, region, refreshPreview]);

  // Rule-card refresh: re-pulls the candidates list for the "Add more" picker.
  // Smart-bypass matches are now decided by the engine at flow time, so there's
  // no detector outcome to surface — keep a simple confirmation toast instead.
  const handleManualScan = useCallback(async () => {
    await refreshCandidates();
    useAlertStore.getState().showAlert(
      t('dashboard:appBypass.rescanRefreshed'),
      'info',
    );
  }, [refreshCandidates, t]);

  // De-dup chain: user-added > installed list.
  const addedIds = useMemo(() => new Set(entries.map((e) => e.id)), [entries]);
  const available = useMemo(
    () => candidates.filter((c) => !addedIds.has(c.id)),
    [candidates, addedIds],
  );

  // Client-side search filter; uses deferred value so keystrokes never wait for filter+render.
  const filteredAvailable = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    if (!q) return available;
    return available.filter(
      (c) => c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q),
    );
  }, [available, deferredSearch]);

  return (
    <Box sx={{ p: 2, maxWidth: 700, mx: 'auto' }}>
      {/* Header */}
      <Stack direction="row" alignItems="center" sx={{ mb: 2 }}>
        <IconButton onClick={() => navigate(-1)} aria-label="back">
          <ArrowBackIcon />
        </IconButton>
        <Typography variant="h6" fontWeight={700} sx={{ flex: 1 }}>
          {t('dashboard:appBypass.title')}
        </Typography>
      </Stack>

      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('dashboard:appBypass.description')}
      </Typography>

      {window._platform?.os === 'macos' && (
        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mb: 2 }}>
          {t('dashboard:appBypass.macMultiUserNote')}
        </Typography>
      )}

      {candidatesError && (
        <Typography color="error" sx={{ mb: 1 }}>
          {t(candidatesError)}
        </Typography>
      )}

      {/* ── SECTION 1: Rule card ── */}
      <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
        <RoutingModeSelector />
        <Divider sx={{ my: 1.5 }} />
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 0.5 }}>
          <AutoAwesomeIcon
            fontSize="small"
            color={smartBypassActive ? 'primary' : 'disabled'}
          />
          <Typography
            variant="body2"
            sx={{ flex: 1 }}
            color={smartBypassActive ? 'text.primary' : 'text.disabled'}
          >
            {smartBypassActive
              ? t('dashboard:appBypass.smartStatus.enabled', { region: (country ?? '').toUpperCase() })
              : t('dashboard:appBypass.smartStatus.disabled')}
          </Typography>
          <IconButton
            size="small"
            onClick={handleManualScan}
            title={t('dashboard:appBypass.ruleCard.refresh')}
          >
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Stack>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
          {t('dashboard:appBypass.ruleCard.manualSummary', { count: entries.length })}
        </Typography>
        {preset === 'global' && (
          <Typography variant="caption" color="warning.main" sx={{ display: 'block', mt: 0.5 }}>
            {t('dashboard:appBypass.ruleCard.globalWarning')}
          </Typography>
        )}
      </Paper>

      {/* ── SECTION 1.5: Detected apps preview (daemon-backed only) ── */}
      {daemonBacked && featureSupported !== false && region && (
        <Box sx={{ mb: 2 }}>
          <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
            <AutoAwesomeIcon fontSize="small" color="primary" />
            <Typography variant="subtitle1" sx={{ flex: 1 }}>
              {t('dashboard:appBypass.preview.section', { count: matched.length })}
            </Typography>
            <IconButton
              size="small"
              onClick={() => void refreshPreview()}
              title={t('dashboard:appBypass.preview.refresh')}
              disabled={matchedLoading}
            >
              <RefreshIcon fontSize="small" />
            </IconButton>
          </Stack>
          {matchedLoading && matched.length === 0 && (
            <Typography variant="caption" color="text.secondary">
              {t('dashboard:appBypass.preview.loading')}
            </Typography>
          )}
          {!matchedLoading && matched.length === 0 && !matchedError && (
            <Typography variant="caption" color="text.secondary">
              {t('dashboard:appBypass.preview.empty')}
            </Typography>
          )}
          {matchedError && (
            <Typography variant="caption" color="error">
              {t(matchedError)}
            </Typography>
          )}
          <Stack spacing={1}>
            {matched.map((m) => (
              <Stack
                key={m.id}
                direction="row"
                alignItems="center"
                spacing={1.5}
                sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
              >
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="body2" fontWeight={600} noWrap>{m.label}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {t(`dashboard:appBypass.preview.hitKind.${m.hit_kind}`)}
                    {m.hit_pattern ? ` — ${m.hit_pattern}` : ''}
                  </Typography>
                </Box>
              </Stack>
            ))}
          </Stack>
        </Box>
      )}

      {/* ── SECTION 2: Manual added ── */}
      {entries.length > 0 && (
        <Box sx={{ mb: 2 }}>
          <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
            <PushPinIcon fontSize="small" color="primary" />
            <Typography variant="subtitle1">
              {t('dashboard:appBypass.manualSection', { count: entries.length })}
            </Typography>
          </Stack>
          <Stack spacing={1}>
            {entries.map((e) => (
              <Stack
                key={e.id}
                direction="row"
                alignItems="center"
                spacing={1.5}
                sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
              >
                <Avatar src={e.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
                  {e.label[0]?.toUpperCase()}
                </Avatar>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="body2" fontWeight={600} noWrap>{e.label}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {t('dashboard:appBypass.processCount', { count: e.names.length })}
                  </Typography>
                </Box>
                {e.kind === 'process' && window._platform?.appList?.listRunning && (
                  <IconButton
                    size="small"
                    title={t('dashboard:appBypass.rescan')}
                    onClick={async () => {
                      try {
                        const cache = useAppBypassStore.getState().candidates;
                        let match = cache.find(
                          (c) => c.kind === 'process' && c.id === e.id,
                        ) as Extract<Candidate, { kind: 'process' }> | undefined;
                        if (!match) {
                          const running = await window._platform!.appList!.listRunning!();
                          const found = running.find((r) => r.id === e.id);
                          if (found) {
                            match = {
                              kind: 'process',
                              id: found.id,
                              label: found.label,
                              processNames: found.processNames,
                              iconUrl: found.iconUrl,
                            };
                          }
                        }
                        if (!match) {
                          useAlertStore.getState().showAlert(
                            t('dashboard:appBypass.loadFailed'),
                            'error',
                          );
                          return;
                        }
                        await useAppBypassStore.getState().rescan(e.id, match.processNames);
                        useAlertStore.getState().showAlert(
                          t('dashboard:appBypass.rescanResult', { count: match.processNames.length }),
                          'success',
                        );
                      } catch {
                        useAlertStore.getState().showAlert(
                          t('dashboard:appBypass.loadFailed'),
                          'error',
                        );
                      }
                    }}
                  >
                    <RefreshIcon fontSize="small" />
                  </IconButton>
                )}
                <Button
                  size="small"
                  color="error"
                  onClick={() => useAppBypassStore.getState().remove(e.id)}
                >
                  {t('dashboard:appBypass.remove')}
                </Button>
              </Stack>
            ))}
          </Stack>
        </Box>
      )}

      {/* ── SECTION 3: Add more ── */}
      <Box>
        <Typography variant="subtitle1" sx={{ mb: 1 }}>
          {t('dashboard:appBypass.addMoreSection')}
        </Typography>

        {!!window._platform?.appList?.listRunning && !window._platform?.appList?.listInstalled && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'block', mb: 1.5 }}
          >
            {t('dashboard:appBypass.runningOnlyHint')}
          </Typography>
        )}

        <TextField
          size="small"
          fullWidth
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t('dashboard:appBypass.searchPlaceholder')}
          sx={{ mb: 1.5 }}
          InputProps={{
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon fontSize="small" />
              </InputAdornment>
            ),
          }}
        />

        {showInitialLoad && candidates.length === 0 && (
          <Stack direction="row" justifyContent="center" sx={{ py: 2 }}>
            <CircularProgress size={20} />
          </Stack>
        )}
        {candidatesLoading && candidates.length > 0 && (
          <LinearProgress sx={{ mb: 1.5, borderRadius: 1 }} />
        )}
        <Stack spacing={1}>
          {filteredAvailable.length === 0 && searchQuery.trim() !== '' && !candidatesLoading && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ p: 2, textAlign: 'center' }}
            >
              {t('dashboard:appBypass.searchEmpty')}
            </Typography>
          )}
          {filteredAvailable.map((c) => (
            <Stack
              key={c.id}
              direction="row"
              alignItems="center"
              spacing={1.5}
              sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
            >
              <Avatar src={c.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
                {c.label[0]?.toUpperCase()}
              </Avatar>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="body2" fontWeight={600} noWrap>{c.label}</Typography>
              </Box>
              <Button
                size="small"
                variant="outlined"
                startIcon={<AddIcon fontSize="small" />}
                onClick={() => {
                  if (c.kind === 'process') {
                    useAppBypassStore.getState().add({
                      id: c.id,
                      label: c.label,
                      kind: 'process',
                      names: c.processNames,
                      iconUrl: c.iconUrl,
                    });
                  } else {
                    useAppBypassStore.getState().add({
                      id: c.id,
                      label: c.label,
                      kind: 'package',
                      names: [c.id],
                      iconUrl: c.iconUrl,
                    });
                  }
                }}
              >
                {t('dashboard:appBypass.addInline')}
              </Button>
            </Stack>
          ))}
        </Stack>
      </Box>
    </Box>
  );
}
