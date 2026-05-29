import { useEffect, useMemo, useState, useDeferredValue } from 'react';
import {
  Box, Typography, Avatar, Chip, Stack, TextField, InputAdornment,
  CircularProgress, Button,
} from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import { useTranslation } from 'react-i18next';
import { useAppRoutesStore, useConfigStore } from '../stores';
import BackButton from '../components/BackButton';
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

  // Running-but-not-installed: the genuine supplement (standalone binaries,
  // brew tools, node…). Dedup by PROCESS NAME, not id — macOS installed.id is
  // the bundle path while running.id is the bundle identifier, so an id compare
  // never matches and every installed app would leak back into this list. Both
  // lists share processNames (executable basenames inside the same bundle).
  const runningExtra = useMemo(() => {
    if (!installed) return [];
    const installedProc = new Set(installed.flatMap((a) => a.processNames));
    return running
      .filter((r) => !r.processNames.some((n) => installedProc.has(n)))
      .filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q));
  }, [installed, running, q]);

  const overrideCount = forceDirect.length + forceProxy.length;

  if (!supported) {
    return (
      <Box sx={{ p: 2, position: 'relative' }}>
        <BackButton to="/" />
        <Typography variant="h6" sx={{ pt: 5 }}>{t('dashboard:appBypass.v2.title')}</Typography>
        <Typography color="text.secondary" sx={{ mt: 2 }}>
          {t('dashboard:appBypass.v2.unsupported')}
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 2, position: 'relative' }}>
      <BackButton to="/" />
      <Typography variant="h6" sx={{ pt: 5 }}>{t('dashboard:appBypass.v2.title')}</Typography>
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

        {runningExtra.length > 0 && (
          <>
            <Typography variant="subtitle2" sx={{ mt: 3, mb: 1 }}>
              {t('dashboard:appBypass.v2.moreSection')}
            </Typography>
            <Stack spacing={0.5}>
              {runningExtra.map((r) => {
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
          </>
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
  // Two spatially-stable chips: proxy always on the left, direct always on the
  // right. The chip matching the region default carries the "默认" prefix and
  // clears any override on click; the opposite chip is the explicit "强制"
  // override. Effective routing = the override, or the region default when none.
  const proxyIsDefault = def === 'proxy';
  const effective = mode === 'default' ? def : mode;
  return (
    <Stack direction="row" alignItems="center" spacing={1} sx={{ py: 0.5 }}>
      <Avatar src={app.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
        {app.label[0]}
      </Avatar>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography noWrap variant="body2">{app.label}</Typography>
      </Box>
      <Stack direction="row" spacing={0.5}>
        <Chip size="small" clickable
          label={proxyIsDefault
            ? t('dashboard:appBypass.v2.chipDefaultProxy')
            : t('dashboard:appBypass.v2.chipForceProxy')}
          color={effective === 'proxy' ? 'primary' : 'default'}
          onClick={() => onSet(proxyIsDefault ? 'default' : 'proxy')} />
        <Chip size="small" clickable
          label={proxyIsDefault
            ? t('dashboard:appBypass.v2.chipForceDirect')
            : t('dashboard:appBypass.v2.chipDefaultDirect')}
          color={effective === 'direct' ? 'primary' : 'default'}
          onClick={() => onSet(proxyIsDefault ? 'direct' : 'default')} />
      </Stack>
    </Stack>
  );
}
