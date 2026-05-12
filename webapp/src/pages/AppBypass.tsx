/**
 * AppBypass — 不走代理的应用 (standalone route, not a keep-alive tab)
 *
 * Skeleton only at Task 6.3. Subsequent tasks add:
 *  - 6.4: list rendering (added + available sections)
 *  - 6.5: manual-add dialog
 *  - 6.6: per-entry rescan button
 *  - 6.7: route registration in App.tsx
 *
 * VPN-guard contract: this page is reachable only when VPN state === 'idle'.
 * If the user (or a background event) flips the state out of idle while the
 * page is mounted, we navigate back to '/' and surface an info toast.
 */
import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box, Typography, Stack, IconButton, CircularProgress,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import { useAppBypassStore, useVPNMachineStore, useAlertStore } from '../stores';

type Candidate =
  | { kind: 'process'; id: string; label: string; processNames: string[]; iconUrl?: string }
  | { kind: 'package'; id: string; label: string; iconUrl?: string };

export default function AppBypass() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const entries = useAppBypassStore((s) => s.entries);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

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
    // Check current state synchronously; if already non-idle, bail out without subscribing.
    const current = useVPNMachineStore.getState().state;
    if (current !== 'idle') {
      onState(current);
      return;
    }
    return useVPNMachineStore.subscribe((s) => s.state, onState);
  }, [navigate, t]);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const appList = window._platform?.appList;
      if (appList?.listInstalled) {
        // Android — package-based list
        const apps = await appList.listInstalled();
        setCandidates(
          apps.map((a) => ({
            kind: 'package',
            id: a.packageName,
            label: a.label,
            iconUrl: a.iconUrl,
          })),
        );
      } else if (appList?.listRunning) {
        // Desktop — process-based list
        const apps = await appList.listRunning();
        setCandidates(
          apps.map((a) => ({
            kind: 'process',
            id: a.id,
            label: a.label,
            processNames: a.processNames,
            iconUrl: a.iconUrl,
          })),
        );
      } else {
        // Platform has no appList provider — leave candidates empty, no error.
        setCandidates([]);
      }
    } catch (e) {
      console.warn('[AppBypass] refresh failed', e);
      setError(t('dashboard:appBypass.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Filter out already-added candidates — used by Task 6.4.
  const addedIds = new Set(entries.map((e) => e.id));
  const available = candidates.filter((c) => !addedIds.has(c.id));
  void available; // referenced by upcoming list-rendering task

  return (
    <Box sx={{ p: 2, maxWidth: 700, mx: 'auto' }}>
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
        <Typography
          variant="caption"
          color="text.disabled"
          sx={{ display: 'block', mb: 2 }}
        >
          {t('dashboard:appBypass.macMultiUserNote')}
        </Typography>
      )}

      {error && (
        <Typography color="error" sx={{ mb: 1 }}>
          {error}
        </Typography>
      )}

      {/* Added section + Available section to be filled in Task 6.4 */}
      <Box>
        <Stack direction="row" alignItems="center" sx={{ mt: 3, mb: 1 }}>
          <Typography variant="subtitle1" sx={{ flex: 1 }}>
            {t('dashboard:appBypass.availableSection')}
          </Typography>
          <IconButton
            onClick={refresh}
            size="small"
            disabled={loading}
            aria-label={t('dashboard:appBypass.refresh')}
          >
            <RefreshIcon />
          </IconButton>
        </Stack>
        {loading && <CircularProgress size={20} />}
      </Box>
    </Box>
  );
}
