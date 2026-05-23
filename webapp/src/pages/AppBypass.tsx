/**
 * AppBypass — 不走代理的应用 (App Bypass page)
 *
 * Layout (4 sections):
 *  1. Rule card  — wraps the existing <RoutingModeSelector /> + count summary +
 *                  global-mode warning + a single refresh action.
 *  2. Smart detection — first 3 detected apps + "查看全部" expander.
 *  3. Manual added — entries the user explicitly added.
 *  4. Add more — search filter + remaining installed apps.
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
import RoutingModeSelector from '../components/RoutingModeSelector';

export default function AppBypass() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const entries = useAppBypassStore((s) => s.entries);
  const autoDetected = useAppBypassStore((s) => s.autoDetected);
  const autoDetectorMeta = useAppBypassStore((s) => s.autoDetectorMeta);
  const candidates = useAppBypassStore((s) => s.candidates);
  const candidatesLoadedAt = useAppBypassStore((s) => s.candidatesLoadedAt);
  const candidatesLoading = useAppBypassStore((s) => s.candidatesLoading);
  const candidatesError = useAppBypassStore((s) => s.candidatesError);
  const refreshCandidates = useAppBypassStore((s) => s.refreshCandidates);
  const preset = useConfigStore((s) => s.resolvePreset());
  const autoActive = preset !== 'global' && window._platform?.os === 'android';
  const [searchQuery, setSearchQuery] = useState('');
  const deferredSearch = useDeferredValue(searchQuery);
  const [autoExpanded, setAutoExpanded] = useState(false);
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

  // Kick off a background refresh on mount; UI renders cached data immediately.
  useEffect(() => {
    refreshCandidates();
  }, [refreshCandidates]);

  // Rule-card refresh: kicks off store refresh + surfaces detector outcome as a toast.
  const handleManualScan = useCallback(async () => {
    await refreshCandidates();
    const { autoDetected: latest, autoDetectorMeta: meta } = useAppBypassStore.getState();
    const country = useConfigStore.getState().country;
    if (!meta) {
      useAlertStore.getState().showAlert(
        t('dashboard:appBypass.rescanResultNoop', { country: country ?? '—' }),
        'info',
      );
    } else if (latest.length === 0) {
      useAlertStore.getState().showAlert(
        t('dashboard:appBypass.rescanResultEmpty'),
        'info',
      );
    } else {
      useAlertStore.getState().showAlert(
        t('dashboard:appBypass.rescanResultDetected', { count: latest.length }),
        'success',
      );
    }
  }, [refreshCandidates, t]);

  // De-dup chain: user-added > auto-detected > installed list.
  const addedIds = useMemo(() => new Set(entries.map((e) => e.id)), [entries]);
  const autoNotAdded = useMemo(
    () => autoDetected.filter((a) => !addedIds.has(a.packageName)),
    [autoDetected, addedIds],
  );
  const autoIds = useMemo(() => new Set(autoNotAdded.map((a) => a.packageName)), [autoNotAdded]);
  const available = useMemo(
    () => candidates.filter((c) => !addedIds.has(c.id) && !autoIds.has(c.id)),
    [candidates, addedIds, autoIds],
  );

  // Client-side search filter; uses deferred value so keystrokes never wait for filter+render.
  const filteredAvailable = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    if (!q) return available;
    return available.filter(
      (c) => c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q),
    );
  }, [available, deferredSearch]);

  // Smart-detection top-3 with expander.
  const autoVisible = autoExpanded ? autoNotAdded : autoNotAdded.slice(0, 3);
  const autoHidden = autoNotAdded.length - 3;

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
        <Stack direction="row" alignItems="center" spacing={1}>
          <Typography variant="body2" color="text.secondary" sx={{ flex: 1 }}>
            {t('dashboard:appBypass.ruleCard.summary', {
              total: entries.length + autoNotAdded.length,
              manual: entries.length,
              auto: autoNotAdded.length,
            })}
          </Typography>
          <IconButton
            size="small"
            onClick={handleManualScan}
            title={t('dashboard:appBypass.ruleCard.refresh')}
          >
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Stack>
        {preset === 'global' && (
          <Typography variant="caption" color="warning.main" sx={{ display: 'block', mt: 0.5 }}>
            {t('dashboard:appBypass.ruleCard.globalWarning')}
          </Typography>
        )}
      </Paper>

      {/* ── SECTION 2: Smart detection (top-3 + expander) ── */}
      {autoDetectorMeta && autoNotAdded.length > 0 && (
        <Box sx={{ mb: 2 }}>
          <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
            <AutoAwesomeIcon fontSize="small" color={autoActive ? 'primary' : 'disabled'} />
            <Typography
              variant="subtitle1"
              sx={{ flex: 1 }}
              color={autoActive ? 'text.primary' : 'text.disabled'}
            >
              {t(autoDetectorMeta.sectionTitleKey, { count: autoNotAdded.length })}
            </Typography>
          </Stack>
          <Stack spacing={1} sx={{ opacity: autoActive ? 1 : 0.55 }}>
            {autoVisible.map((a) => (
              <Stack
                key={a.packageName}
                direction="row"
                alignItems="center"
                spacing={1.5}
                sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
              >
                <Avatar src={a.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
                  {a.label[0]?.toUpperCase()}
                </Avatar>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="body2" fontWeight={600} noWrap>{a.label}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {t(a.reasonKey)}
                  </Typography>
                </Box>
              </Stack>
            ))}
            {autoNotAdded.length > 3 && (
              <Button
                size="small"
                onClick={() => setAutoExpanded(!autoExpanded)}
                sx={{ alignSelf: 'flex-start' }}
              >
                {autoExpanded
                  ? t('dashboard:appBypass.smartDetection.collapse')
                  : t('dashboard:appBypass.smartDetection.showAll', { count: autoHidden })}
              </Button>
            )}
          </Stack>
        </Box>
      )}

      {/* ── SECTION 3: Manual added ── */}
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
                        const running = await window._platform!.appList!.listRunning!();
                        const match = running.find((r) => r.id === e.id);
                        if (!match) return;
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

      {/* ── SECTION 4: Add more ── */}
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
