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

// Windows install-dir / exe-path ids look like `C:\…`; macOS ids are bundle
// paths, Android ids are package names — only the Windows shape gets the
// path-prefix folding below.
const isWindowsPath = (s: string) => /^[a-zA-Z]:[\\/]/.test(s);
const normWinPath = (s: string) => s.replace(/\//g, '\\').toLowerCase();

// Fold running processes whose exe path sits under an installed app's install
// directory (Windows: installed.id IS the directory, running.id IS the exe
// path) into that app: their basenames extend the app's processNames, and the
// claimed rows drop out of the "more — running" section. This is what lets one
// toggle cover every exe the app actually runs, including ones the registry
// scan's depth bound missed.
function foldRunningIntoInstalled(installed: InstalledApp[], running: RunningApp[]): {
  merged: InstalledApp[];
  claimedRunningIds: Set<string>;
} {
  const claimed = new Set<string>();
  const merged = installed.map((app) => {
    if (!isWindowsPath(app.id)) return app;
    const dir = normWinPath(app.id).replace(/\\+$/, '') + '\\';
    const extra: string[] = [];
    for (const r of running) {
      if (!isWindowsPath(r.id) || !normWinPath(r.id).startsWith(dir)) continue;
      claimed.add(r.id);
      for (const n of r.processNames) {
        if (!app.processNames.includes(n) && !extra.includes(n)) extra.push(n);
      }
    }
    return extra.length ? { ...app, processNames: [...app.processNames, ...extra] } : app;
  });
  return { merged, claimedRunningIds: claimed };
}

export default function AppBypass() {
  const { t } = useTranslation();
  const country = useConfigStore((s) => s.country);
  const overrides = useAppRoutesStore((s) => s.overrides);
  const classifications = useAppRoutesStore((s) => s.classifications);
  const classifyInstalled = useAppRoutesStore((s) => s.classifyInstalled);
  const setOverride = useAppRoutesStore((s) => s.setOverride);
  const refreshOverrideNames = useAppRoutesStore((s) => s.refreshOverrideNames);
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

  const { merged, claimedRunningIds } = useMemo(
    () => foldRunningIntoInstalled(installed ?? [], running),
    [installed, running],
  );

  // A previously-saved override may cover fewer exes than we now know the app
  // has (deeper scan, running exes folded in) — re-sync so the user's earlier
  // choice covers the whole app without a re-toggle.
  useEffect(() => {
    if (merged.length) void refreshOverrideNames(merged);
  }, [merged, refreshOverrideNames]);

  const filtered = useMemo(() => {
    if (!installed) return [];
    if (!q) return merged;
    return merged.filter((a) => a.label.toLowerCase().includes(q) || a.id.toLowerCase().includes(q));
  }, [installed, merged, q]);

  // Running-but-not-installed: the genuine supplement (standalone binaries,
  // brew tools, node…). Rows already folded into an installed app by path are
  // claimed; the rest dedup by PROCESS NAME, not id — macOS installed.id is
  // the bundle path while running.id is the bundle identifier, so an id compare
  // never matches and every installed app would leak back into this list. Both
  // lists share processNames (executable basenames inside the same bundle).
  const runningExtra = useMemo(() => {
    if (!installed) return [];
    const installedProc = new Set(merged.flatMap((a) => a.processNames));
    return running
      .filter((r) => !claimedRunningIds.has(r.id))
      .filter((r) => !r.processNames.some((n) => installedProc.has(n)))
      .filter((r) => !q || r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q));
  }, [installed, merged, claimedRunningIds, running, q]);

  const overrideCount = Object.keys(overrides).length;

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
                mode={overrides[app.id]?.mode ?? 'default'}
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
                    mode={overrides[r.id]?.mode ?? 'default'}
                    onSet={(m) => void setOverride(rApp, m)}
                    subtitle={r.id}
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

function AppRow({ app, def, mode, onSet, subtitle }: {
  app: InstalledApp;
  def: 'direct' | 'proxy';
  mode: OverrideMode;
  onSet: (m: OverrideMode) => void;
  // Running-process rows pass the executable path here. Override identity is
  // by app id (path), but the ENGINE matches by process NAME — two binaries
  // sharing a basename (e.g. /usr/bin/curl and /opt/homebrew/bin/curl) show
  // as separate rows yet route together; the path is what tells them apart.
  // Installed-app rows omit this and stay single-line.
  subtitle?: string;
}) {
  const { t } = useTranslation();
  // Three spatially-stable chips: 智能 (follow the region default), 代理
  // (explicit force-proxy), 直连 (explicit force-direct). The earlier two-chip
  // design had no force-proxy control at all for apps whose default was
  // already proxy — exactly the apps (Google Play, system Settings) users
  // asked to pin to the proxy so region-CN direct rules can't peel their
  // traffic off the tunnel (tickets #3276/#3369) — and the chip that DID
  // write force-proxy was labeled 智能. One chip per state fixes both.
  //
  // Highlight: the explicit override chip when one is set; otherwise 智能 is
  // active and the chip matching the region default gets an outlined hint so
  // the user can see what 智能 currently resolves to.
  return (
    <Stack direction="row" alignItems="center" spacing={1} sx={{ py: 0.5 }}>
      <Avatar src={app.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
        {app.label[0]}
      </Avatar>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography noWrap variant="body2">{app.label}</Typography>
        {subtitle && (
          <Typography noWrap variant="caption" color="text.secondary" sx={{ display: 'block' }}>
            {subtitle}
          </Typography>
        )}
      </Box>
      <Stack direction="row" spacing={0.5}>
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipSmart')}
          color={mode === 'default' ? 'primary' : 'default'}
          onClick={() => onSet('default')} />
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipProxy')}
          color={mode === 'proxy' || (mode === 'default' && def === 'proxy') ? 'primary' : 'default'}
          variant={mode === 'default' && def === 'proxy' ? 'outlined' : 'filled'}
          onClick={() => onSet('proxy')} />
        <Chip size="small" clickable
          label={t('dashboard:appBypass.v2.chipDirect')}
          color={mode === 'direct' || (mode === 'default' && def === 'direct') ? 'primary' : 'default'}
          variant={mode === 'default' && def === 'direct' ? 'outlined' : 'filled'}
          onClick={() => onSet('direct')} />
      </Stack>
    </Stack>
  );
}
