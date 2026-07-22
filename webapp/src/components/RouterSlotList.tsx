/**
 * RouterSlotList — enterprise multi-slot router form.
 *
 * Renders k2r's per-slot manifest (see vpn-types.ts RouterSlot): one row
 * per bound line with rename/password self-service actions, plus a
 * fail-closed alarm indicator. Disabled (unbound) slots are display-only.
 */
import { useState } from 'react';
import {
  List, ListItem, ListItemText, Box, Typography, IconButton,
  Dialog, DialogTitle, DialogContent, DialogActions, TextField, Button,
} from '@mui/material';
import { Edit as RenameIcon, Lock as PasswordIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import type { RouterSlot } from '../services/vpn-types';
import { gatewaySetSlotSsid, gatewaySetSlotPassword } from '../services/gateway-core';

interface RouterSlotListProps {
  slots: RouterSlot[];
}

interface RouterDeviceLike {
  ip: string;
}

/** Groups devices by their subnet's third octet (`10.81.N.x` → slot N). Any
 * other address (e.g. management LAN) falls into the "management" bucket. */
export function groupDevicesBySlot<T extends RouterDeviceLike>(
  devices: T[],
  slots: RouterSlot[],
): { slot: RouterSlot | null; label: string; devices: T[] }[] {
  const bySlot = new Map<number, T[]>();
  const management: T[] = [];
  for (const device of devices) {
    const match = /^10\.81\.(\d+)\./.exec(device.ip);
    const slotNo = match ? parseInt(match[1], 10) : null;
    if (slotNo !== null && slots.some((s) => s.slot === slotNo)) {
      const list = bySlot.get(slotNo) ?? [];
      list.push(device);
      bySlot.set(slotNo, list);
    } else {
      management.push(device);
    }
  }
  const groups: { slot: RouterSlot | null; label: string; devices: T[] }[] = slots
    .filter((s) => bySlot.has(s.slot))
    .map((s) => ({ slot: s, label: `${s.country.toUpperCase()}-${s.index}`, devices: bySlot.get(s.slot)! }));
  if (management.length > 0) {
    groups.push({ slot: null, label: 'management', devices: management });
  }
  return groups;
}

function formatDownFor(downSince: string | undefined): string {
  if (!downSince) return '';
  const ms = Date.now() - new Date(downSince).getTime();
  const minutes = Math.max(0, Math.floor(ms / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

export default function RouterSlotList({ slots }: RouterSlotListProps) {
  const { t } = useTranslation();
  const [renameSlot, setRenameSlot] = useState<RouterSlot | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renameError, setRenameError] = useState('');
  const [passwordSlot, setPasswordSlot] = useState<RouterSlot | null>(null);
  const [passwordValue, setPasswordValue] = useState('');
  const [passwordError, setPasswordError] = useState('');

  const openRename = (slot: RouterSlot) => {
    setRenameValue(slot.ssid);
    setRenameError('');
    setRenameSlot(slot);
  };

  const openPassword = (slot: RouterSlot) => {
    setPasswordValue('');
    setPasswordError('');
    setPasswordSlot(slot);
  };

  const confirmRename = async () => {
    if (!renameSlot) return;
    if (renameValue.length < 1 || renameValue.length > 32) {
      setRenameError(t('dashboard:routerSlots.renameHint'));
      return;
    }
    const res = await gatewaySetSlotSsid(renameSlot.slot, renameValue);
    if (res.code === 0) {
      setRenameSlot(null);
    } else {
      setRenameError(t('dashboard:routerSlots.applyFailed'));
    }
  };

  const confirmPassword = async () => {
    if (!passwordSlot) return;
    if (passwordValue.length < 8 || passwordValue.length > 63) {
      setPasswordError(t('dashboard:routerSlots.passwordHint'));
      return;
    }
    const res = await gatewaySetSlotPassword(passwordSlot.slot, passwordValue);
    if (res.code === 0) {
      setPasswordSlot(null);
    } else {
      setPasswordError(t('dashboard:routerSlots.applyFailed'));
    }
  };

  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="h6" fontWeight={700} sx={{ px: 1, mb: 1 }}>
        {t('dashboard:routerSlots.title')}
      </Typography>
      <List disablePadding>
        {slots.map((slot) => {
          const isDisabled = slot.state === 'disabled';
          const isFailClosed = slot.state === 'failClosed';
          const lineLabel = slot.country ? `${slot.country.toUpperCase()}-${slot.index}` : '';
          const ssidLabel = isDisabled
            ? t('dashboard:routerSlots.unbound')
            : slot.ssid || t('dashboard:routerSlots.customName');
          return (
            <ListItem
              key={slot.slot}
              divider
              secondaryAction={
                !isDisabled && (
                  <Box sx={{ display: 'flex', gap: 0.5 }}>
                    <IconButton
                      size="small"
                      data-testid={`slot-${slot.slot}-rename`}
                      onClick={() => openRename(slot)}
                    >
                      <RenameIcon fontSize="small" />
                    </IconButton>
                    <IconButton
                      size="small"
                      data-testid={`slot-${slot.slot}-password`}
                      onClick={() => openPassword(slot)}
                    >
                      <PasswordIcon fontSize="small" />
                    </IconButton>
                  </Box>
                )
              }
            >
              <Box
                sx={{
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  mr: 1.5,
                  flexShrink: 0,
                  bgcolor: isDisabled ? 'text.disabled' : isFailClosed ? 'error.main' : 'success.main',
                }}
                data-testid={isFailClosed ? `slot-${slot.slot}-alarm` : isDisabled ? `slot-${slot.slot}-disabled` : undefined}
              />
              <ListItemText
                primary={ssidLabel}
                secondary={
                  isFailClosed
                    ? `${lineLabel} · ${t('dashboard:routerSlots.failClosed')}${slot.downSince ? ` · ${t('dashboard:routerSlots.downFor', { duration: formatDownFor(slot.downSince) })}` : ''}`
                    : lineLabel
                }
                primaryTypographyProps={{ sx: { color: isDisabled ? 'text.disabled' : 'text.primary' } }}
                secondaryTypographyProps={{ sx: { color: isFailClosed ? 'error.main' : 'text.secondary' } }}
              />
            </ListItem>
          );
        })}
      </List>

      {/* Rename dialog */}
      <Dialog open={!!renameSlot} onClose={() => setRenameSlot(null)} maxWidth="xs" fullWidth>
        <DialogTitle>{t('dashboard:routerSlots.rename')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            fullWidth
            data-testid="slot-rename-input"
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            helperText={renameError || t('dashboard:routerSlots.renameHint')}
            error={!!renameError}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRenameSlot(null)}>{t('dashboard:routerSlots.cancel')}</Button>
          <Button variant="contained" data-testid="slot-rename-confirm" onClick={confirmRename}>
            {t('dashboard:routerSlots.confirm')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Password dialog */}
      <Dialog open={!!passwordSlot} onClose={() => setPasswordSlot(null)} maxWidth="xs" fullWidth>
        <DialogTitle>{t('dashboard:routerSlots.password')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            fullWidth
            type="password"
            data-testid="slot-password-input"
            value={passwordValue}
            onChange={(e) => setPasswordValue(e.target.value)}
            helperText={passwordError || t('dashboard:routerSlots.passwordHint')}
            error={!!passwordError}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPasswordSlot(null)}>{t('dashboard:routerSlots.cancel')}</Button>
          <Button variant="contained" data-testid="slot-password-confirm" onClick={confirmPassword}>
            {t('dashboard:routerSlots.confirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
