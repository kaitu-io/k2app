import { useEffect, useMemo, useState, useDeferredValue } from 'react';
import {
  Box, Typography, Avatar, Chip, Stack, TextField, InputAdornment,
  CircularProgress, Button, Accordion, AccordionSummary, AccordionDetails,
} from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useTranslation } from 'react-i18next';
import { useAppRoutesStore, useConfigStore } from '../stores';
import ConnectedSettingsLock from '../components/ConnectedSettingsLock';
import type { InstalledApp, RunningApp } from '../types/kaitu-core';

type OverrideMode = 'direct' | 'proxy' | 'default';

// Identity is by PROCESS NAME (what the engine match.apps matches), not id.
// An app reads as overridden only when ALL its process names are in the set.
function modeOf(app: { processNames: string[] }, forceDirect: string[], forceProxy: string[]): OverrideMode {
  const names = app.processNames ?? [];
  if (names.length > 0 && names.every((n) => forceDirect.includes(n))) return 'direct';
  if (names.length > 0 && names.every((n) => forceProxy.includes(n))) return 'proxy';
  return 'default';
}

export default function AppBypass() {
  const { t } = useTranslation();
  const country = useConfigStore((s) => s.country);
  const forceDirect = useAppRoutesStore((s) => s.forceDirect);
  const forceProxy = useAppRoutesStore((s) => s.forceProxy);
  const classifications = useAppRoutesStore((s) => s.classifications);
  const classifyInstalled = useAppRoutesStore((s) => s.classifyInstalled);
  const setOverride = useAppRoutesStore((s) => s.setOverride);
  const resetOverrides = useAppRoutesStore((s) => s.resetOverrides);

  const listInstalled = window._platform?.appList?.listInstalled;
  const listRunning = window._platform?.appList?.listRunning;
  // Supported if EITHER enumerator exists. Linux (standalone bridge) has only
  // listRunning — running apps become the primary list there. iOS has neither.
  const supported = !!(listInstalled || listRunning);
  const [installed, setInstalled] = useState<InstalledApp[] | null>(null);
  const [running, setRunning] = useState<RunningApp[]>([]);
  const [search, setSearch] = useState('');
  const q = useDeferredValue(search).toLowerCase();

  useEffect(() => {
    if (!supported) return;
    let alive = true;
    (async () => {
      // Primary source: installed apps if available, else running processes
      // (Linux). Both produce {id,label,processNames,iconUrl?} rows.
      const primary = listInstalled ?? listRunning!;
      const apps = ((await primary()) ?? []) as InstalledApp[];
      if (!alive) return;
      setInstalled(apps);
      await classifyInstalled(country ?? '', apps);
      // The "more — running" section only exists when installed IS primary
      // (desktop/Android). On running-only platforms there is nothing extra.
      if (listInstalled && listRunning) setRunning((await listRunning()) ?? []);
    })();
    return () => { alive = false; };
  }, [supported, country, classifyInstalled]);

  const filtered = useMemo(() => {
    if (!installed) return [];
    if (!q) return installed;
    return installed.filter((a) => a.label.toLowerCase().includes(q) || a.id.toLowerCase().includes(q));
  }, [installed, q]);

  const overrideCount = forceDirect.length + forceProxy.length;

  if (!supported) {
    return (
      <Box sx={{ p: 2 }}>
        <Typography variant="h6">{t('dashboard:appBypass.v2.title')}</Typography>
        <Typography color="text.secondary" sx={{ mt: 2 }}>
          {t('dashboard:appBypass.v2.unsupported')}
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 2 }}>
      <Typography variant="h6">{t('dashboard:appBypass.v2.title')}</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('dashboard:appBypass.v2.intro')}
      </Typography>

      <ConnectedSettingsLock>
        <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 1 }}>
          <Typography variant="subtitle2">
            {t('dashboard:appBypass.v2.installedSection', { count: installed?.length ?? 0 })}
          </Typography>
          {overrideCount > 0 && (
            <Button size="small" onClick={() => void resetOverrides()}>
              {t('dashboard:appBypass.v2.reset')}
            </Button>
          )}
        </Stack>

        <TextField
          fullWidth size="small" value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('dashboard:appBypass.v2.search')}
          InputProps={{ startAdornment: <InputAdornment position="start"><SearchIcon /></InputAdornment> }}
          sx={{ mb: 1 }}
        />

        {installed === null ? (
          <Stack alignItems="center" sx={{ py: 4 }}>
            <CircularProgress size={24} />
            <Typography variant="caption" sx={{ mt: 1 }}>{t('dashboard:appBypass.v2.loading')}</Typography>
          </Stack>
        ) : filtered.length === 0 ? (
          <Typography color="text.secondary" sx={{ py: 2 }}>{t('dashboard:appBypass.v2.empty')}</Typography>
        ) : (
          <Stack spacing={0.5}>
            {filtered.map((app) => (
              <AppRow
                key={app.id}
                app={app}
                def={classifications.get(app.id) ?? 'proxy'}
                mode={modeOf(app, forceDirect, forceProxy)}
                onSet={(m) => void setOverride(app, m)}
              />
            ))}
          </Stack>
        )}

        {running.length > 0 && (
          <Accordion sx={{ mt: 2 }} disableGutters>
            <AccordionSummary expandIcon={<ExpandMoreIcon />}>
              <Typography variant="subtitle2">{t('dashboard:appBypass.v2.moreSection')}</Typography>
            </AccordionSummary>
            <AccordionDetails>
              <Stack spacing={0.5}>
                {running
                  .filter((r) => !installed?.some((a) => a.id === r.id))
                  .map((r) => {
                    const rApp: InstalledApp = {
                      id: r.id, label: r.label, processNames: r.processNames, iconUrl: r.iconUrl,
                    };
                    return (
                      <AppRow
                        key={r.id}
                        app={rApp}
                        def={classifications.get(r.id) ?? 'proxy'}
                        mode={modeOf(rApp, forceDirect, forceProxy)}
                        onSet={(m) => void setOverride(rApp, m)}
                      />
                    );
                  })}
              </Stack>
            </AccordionDetails>
          </Accordion>
        )}
      </ConnectedSettingsLock>
    </Box>
  );
}

function AppRow({ app, def, mode, onSet }: {
  app: InstalledApp;
  def: 'direct' | 'proxy';
  mode: OverrideMode;
  onSet: (m: OverrideMode) => void;
}) {
  const { t } = useTranslation();
  return (
    <Stack direction="row" alignItems="center" spacing={1} sx={{ py: 0.5 }}>
      <Avatar src={app.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
        {app.label[0]}
      </Avatar>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography noWrap variant="body2">{app.label}</Typography>
        <Chip
          size="small" variant="outlined"
          label={def === 'direct'
            ? t('dashboard:appBypass.v2.badgeDirect')
            : t('dashboard:appBypass.v2.badgeProxy')}
          color={def === 'direct' ? 'success' : 'default'}
          sx={{ height: 18, fontSize: 11 }}
        />
      </Box>
      <Stack direction="row" spacing={0.5}>
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipDefault')}
          color={mode === 'default' ? 'primary' : 'default'}
          onClick={() => onSet('default')} />
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipForceDirect')}
          color={mode === 'direct' ? 'success' : 'default'}
          onClick={() => onSet('direct')} />
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipForceProxy')}
          color={mode === 'proxy' ? 'warning' : 'default'}
          onClick={() => onSet('proxy')} />
      </Stack>
    </Stack>
  );
}
