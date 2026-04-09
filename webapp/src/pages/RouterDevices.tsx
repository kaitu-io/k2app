/**
 * RouterDevices — Gateway-only LAN device management page.
 *
 * First-class keep-alive tab: Dashboard | Router | Purchase | ...
 * Only mounted when platformType === 'gateway'.
 *
 * Displays discovered LAN devices, manages MAC allowlist,
 * and controls open/allowlist mode.
 *
 * Calls gateway HTTP API directly (/api/router-devices/*).
 * These are LOCAL gateway endpoints, NOT cloud API.
 */
import { useState, useEffect, useCallback } from 'react';
import {
  Box, Typography, Stack, Card, Chip, Switch, Button,
  IconButton, TextField, Dialog, DialogTitle, DialogContent,
  DialogActions, CircularProgress, Alert,
} from '@mui/material';
import {
  CheckCircle as AllowedIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Refresh as RefreshIcon,
  Wifi as OnlineIcon,
  WifiOff as OfflineIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

interface RouterDevice {
  mac: string;
  ip: string;
  hostname: string;
  online: boolean;
  allowed: boolean;
  remark: string;
}

interface RouterDeviceList {
  mode: 'open' | 'allowlist';
  quota: number;
  used: number;
  routerDevices: RouterDevice[];
}

// Direct gateway HTTP calls (not cloudApi — local endpoints)
async function gwFetch<T>(path: string, opts?: RequestInit): Promise<{ code: number; message?: string; data?: T }> {
  try {
    const resp = await fetch(path, {
      headers: { 'Content-Type': 'application/json' },
      ...opts,
    });
    return await resp.json();
  } catch {
    return { code: -1, message: 'Gateway unreachable' };
  }
}

export default function RouterDevices() {
  const { t } = useTranslation();
  const [data, setData] = useState<RouterDeviceList | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [remarkDialogOpen, setRemarkDialogOpen] = useState(false);
  const [remarkTarget, setRemarkTarget] = useState<{ mac: string; remark: string }>({ mac: '', remark: '' });

  const fetchDevices = useCallback(async () => {
    setLoading(true);
    const res = await gwFetch<RouterDeviceList>('/api/router-devices');
    if (res.code === 0 && res.data) {
      setData(res.data);
      setError('');
    } else {
      setError(res.message || 'Failed to load devices');
    }
    setLoading(false);
  }, []);

  useEffect(() => { fetchDevices(); }, [fetchDevices]);

  const handleModeToggle = async () => {
    if (!data) return;
    const newMode = data.mode === 'open' ? 'allowlist' : 'open';
    const res = await gwFetch('/api/router-devices/mode', {
      method: 'POST',
      body: JSON.stringify({ mode: newMode }),
    });
    if (res.code === 0) fetchDevices();
  };

  const handleAllow = async (mac: string, remark: string) => {
    const res = await gwFetch('/api/router-devices/allow', {
      method: 'POST',
      body: JSON.stringify({ mac, remark }),
    });
    if (res.code === 0) {
      fetchDevices();
    } else if (res.message === 'quotaExceeded') {
      setError(t('dashboard:routerDevices.quotaExceeded'));
    }
  };

  const handleRemove = async (mac: string) => {
    const res = await gwFetch('/api/router-devices/remove', {
      method: 'POST',
      body: JSON.stringify({ mac }),
    });
    if (res.code === 0) fetchDevices();
  };

  if (loading && !data) {
    return <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}><CircularProgress /></Box>;
  }

  if (!data) {
    return <Alert severity="error" sx={{ m: 2 }}>{error}</Alert>;
  }

  const isAllowlist = data.mode === 'allowlist';
  const quotaText = data.quota <= 0 ? t('dashboard:routerDevices.unlimited') : `${data.used}/${data.quota}`;

  return (
    <Box sx={{ p: 2, maxWidth: 600, mx: 'auto' }}>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 2 }}>
        <Typography variant="h6" fontWeight={700}>
          {t('dashboard:routerDevices.title')}
        </Typography>
        <IconButton onClick={fetchDevices} size="small"><RefreshIcon /></IconButton>
      </Stack>

      {/* Mode toggle */}
      <Card variant="outlined" sx={{ p: 2, mb: 2 }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between">
          <Box>
            <Typography variant="body1" fontWeight={600}>
              {t('dashboard:routerDevices.mode')}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {isAllowlist
                ? t('dashboard:routerDevices.modeAllowlistDesc')
                : t('dashboard:routerDevices.modeOpenDesc')}
            </Typography>
          </Box>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Chip
              label={isAllowlist ? t('dashboard:routerDevices.allowlist') : t('dashboard:routerDevices.open')}
              color={isAllowlist ? 'warning' : 'success'}
              size="small"
            />
            <Switch checked={isAllowlist} onChange={handleModeToggle} />
          </Stack>
        </Stack>
        {isAllowlist && (
          <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
            {t('dashboard:routerDevices.quotaUsage', { usage: quotaText })}
          </Typography>
        )}
      </Card>

      {error && <Alert severity="warning" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}

      {/* Device list */}
      <Stack spacing={1}>
        {data.routerDevices.map(device => (
          <Card key={device.mac} variant="outlined" sx={{ p: 1.5 }}>
            <Stack direction="row" alignItems="center" spacing={1.5}>
              {device.online ? <OnlineIcon color="success" fontSize="small" /> : <OfflineIcon color="disabled" fontSize="small" />}
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="body2" fontWeight={600} noWrap>
                  {device.hostname || device.mac}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {device.ip || device.mac}{device.remark ? ` · ${device.remark}` : ''}
                </Typography>
              </Box>
              {device.allowed ? (
                <Stack direction="row" alignItems="center" spacing={0.5}>
                  <AllowedIcon color="success" fontSize="small" />
                  <IconButton size="small" onClick={() => handleRemove(device.mac)}>
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                </Stack>
              ) : (
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<AddIcon />}
                  onClick={() => {
                    setRemarkTarget({ mac: device.mac, remark: '' });
                    setRemarkDialogOpen(true);
                  }}
                >
                  {t('dashboard:routerDevices.allow')}
                </Button>
              )}
            </Stack>
          </Card>
        ))}
      </Stack>

      {/* Remark dialog */}
      <Dialog open={remarkDialogOpen} onClose={() => setRemarkDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>{t('dashboard:routerDevices.allowDevice')}</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {remarkTarget.mac}
          </Typography>
          <TextField
            autoFocus
            fullWidth
            label={t('dashboard:routerDevices.remark')}
            value={remarkTarget.remark}
            onChange={e => setRemarkTarget(prev => ({ ...prev, remark: e.target.value }))}
            placeholder={t('dashboard:routerDevices.remarkPlaceholder')}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRemarkDialogOpen(false)}>{t('common:cancel')}</Button>
          <Button
            variant="contained"
            onClick={() => {
              handleAllow(remarkTarget.mac, remarkTarget.remark);
              setRemarkDialogOpen(false);
            }}
          >
            {t('dashboard:routerDevices.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
