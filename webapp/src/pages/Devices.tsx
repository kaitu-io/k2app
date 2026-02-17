import React, { useEffect, useState, useRef } from "react";
import {
  Box,
  Typography,
  List,
  ListItem,
  ListItemText,
  ListItemSecondaryAction,
  IconButton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Tooltip,
  CircularProgress,
} from "@mui/material";
import {
  Delete as DeleteIcon,
  Computer as ComputerIcon,
  EditOutlined as EditIcon,
} from "@mui/icons-material";
import BackButton from "../components/BackButton";
import { useTranslation } from "react-i18next";
import { formatTime } from "../utils/time";
import { Device } from "../services/api-types";

import { useUser } from "../hooks/useUser";
import { LoadingCard, EmptyDevices } from "../components/LoadingAndEmpty";
import { k2api } from '../services/k2api';
import { delayedFocus } from '../utils/ui';

export default function Devices() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deviceToDelete, setDeviceToDelete] = useState<Device | null>(null);
  const [editingUdid, setEditingUdid] = useState<string | null>(null);
  const [editingRemark, setEditingRemark] = useState<string>("");
  const [savingRemark, setSavingRemark] = useState(false);
  const { user } = useUser();
  const currentUdid = user?.device?.udid;

  // Ref for delayed focus when editing device remark
  const remarkInputRef = useRef<HTMLInputElement>(null);

  // Delayed focus when entering edit mode
  useEffect(() => {
    if (!editingUdid) return;
    const cancel = delayedFocus(() => remarkInputRef.current, 100);
    return cancel;
  }, [editingUdid]);

  useEffect(() => {
    loadDevices();
  }, []);

  const loadDevices = async () => {
    setLoading(true);
    try {
      console.debug(t('account:devices.loadDeviceListStart'));
      const response = await k2api().exec<{ items: Device[] }>('api_request', {
        method: 'GET',
        path: '/api/user/devices',
      });
      if (response.code !== 0 || !response.data) {
        console.error('[Devices] Load device list failed:', response.code, response.message);
        window._platform?.showToast?.(
          t('account:devices.loadDeviceListFailed'),
          'error'
        );
        return;
      }
      setDevices(response.data.items || []);
      console.info('[Devices] Load device list success');
    } catch (err) {
      console.error('[Devices] Load device list failed:', err);
      window._platform?.showToast?.(
        t('account:devices.loadDeviceListFailed'),
        'error'
      );
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteClick = (device: Device) => {
    setDeviceToDelete(device);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!deviceToDelete) return;

    try {
      console.debug(t('account:devices.deleteDeviceStart'));
      const response = await k2api().exec('api_request', {
        method: 'DELETE',
        path: `/api/user/devices/${deviceToDelete.udid}`,
      });
      if (response.code !== 0) {
        console.error('[Devices] Delete device failed:', response.code, response.message);
        window._platform?.showToast?.(
          t('account:devices.deleteDeviceFailed'),
          'error'
        );
        return;
      }
      await loadDevices();
      console.info('[Devices] Delete device success');
      window._platform?.showToast?.(
        t('account:devices.deleteDeviceSuccess'),
        'success'
      );
    } catch (err) {
      console.error('[Devices] Delete device failed:', err);
      window._platform?.showToast?.(
        t('account:devices.deleteDeviceFailed'),
        'error'
      );
    } finally {
      setDeleteDialogOpen(false);
      setDeviceToDelete(null);
    }
  };

  const handleEditRemark = (device: Device) => {
    setEditingUdid(device.udid);
    setEditingRemark(device.remark);
  };

  const handleRemarkChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setEditingRemark(e.target.value);
  };

  const handleRemarkSave = async (device: Device) => {
    if (editingRemark.trim() === "" || editingRemark === device.remark) {
      setEditingUdid(null);
      return;
    }
    setSavingRemark(true);
    try {
      const response = await k2api().exec('api_request', {
        method: 'PUT',
        path: `/api/user/devices/${device.udid}/remark`,
        body: { remark: editingRemark.trim() },
      });
      if (response.code !== 0) {
        console.error('[Devices] Update remark failed:', response.code, response.message);
        window._platform?.showToast?.(
          t('account:devices.updateRemarkFailed'),
          'error'
        );
        return;
      }
      await loadDevices();
      window._platform?.showToast?.(
        t('account:devices.updateRemarkSuccess'),
        'success'
      );
    } catch (err) {
      console.error('[Devices] Update remark failed:', err);
      window._platform?.showToast?.(
        t('account:devices.updateRemarkFailed'),
        'error'
      );
    } finally {
      setSavingRemark(false);
      setEditingUdid(null);
    }
  };

  return (
    <Box sx={{
      width: "100%",
      py: 0.5,
      backgroundColor: "transparent",
      position: "relative"
    }}>
      <BackButton to="/account" />
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5, px: 1, pt: 7 }}>
        <Typography variant="h6" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t('account:devices.title')}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ px: 1 }}>
          <LoadingCard message={t('account:devices.loading')} />
        </Box>
      ) : devices.length === 0 ? (
        <Box sx={{ px: 1, backgroundColor: (theme) => theme.palette.background.paper, borderRadius: 2 }}>
          <EmptyDevices />
        </Box>
      ) : (
        <Box sx={{
          backgroundColor: (theme) => theme.palette.background.paper,
          borderRadius: 2,
        }}>
          <List>
            {devices.map((device) => (
              <ListItem key={device.udid}>
                <ListItemText
                  primary={
                    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                      {editingUdid === device.udid ? (
                        <TextField
                          value={editingRemark}
                          onChange={handleRemarkChange}
                          onBlur={() => handleRemarkSave(device)}
                          onKeyDown={e => {
                            if (e.key === 'Enter') {
                              handleRemarkSave(device);
                            } else if (e.key === 'Escape') {
                              setEditingUdid(null);
                            }
                          }}
                          size="small"
                          inputRef={remarkInputRef}
                          disabled={savingRemark}
                          InputProps={{
                            endAdornment: savingRemark ? (
                              <CircularProgress size={16} sx={{ ml: 1 }} />
                            ) : null,
                          }}
                          inputProps={{
                            autoCapitalize: "sentences",
                            autoCorrect: "on",
                            spellCheck: true,
                          }}
                          sx={{ minWidth: 120 }}
                        />
                      ) : (
                        <Tooltip title={t('account:devices.clickEditRemark')} arrow>
                          <Box sx={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }} onClick={() => handleEditRemark(device)}>
                            <Typography
                              variant="body1"
                              sx={{ mr: 0.5 }}
                            >
                              {device.remark || t('account:devices.unnamedDevice')}
                            </Typography>
                            <EditIcon sx={{ fontSize: 18, color: 'action.active', opacity: 0.7 }} />
                          </Box>
                        </Tooltip>
                      )}
                    </Box>
                  }
                  secondary={
                    <Box sx={{ display: "flex", alignItems: "center", gap: 1, mt: 0.5 }}>
                      <Typography variant="body2" color="text.secondary" component="span">
                        {`${t('account:devices.lastLoginPrefix')}${formatTime(device.tokenLastUsedAt)}`}
                      </Typography>
                      {device.udid === currentUdid && (
                        <Chip
                          icon={<ComputerIcon />}
                          label={t('account:devices.currentDevice')}
                          size="small"
                          color="primary"
                          variant="outlined"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </Box>
                  }
                  secondaryTypographyProps={{ component: 'div' }}
                />
                {device.udid !== currentUdid && (
                  <ListItemSecondaryAction>
                    <IconButton
                      edge="end"
                      aria-label="delete"
                      onClick={() => handleDeleteClick(device)}
                    >
                      <DeleteIcon />
                    </IconButton>
                  </ListItemSecondaryAction>
                )}
              </ListItem>
            ))}
          </List>
        </Box>
      )}

      <Dialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
      >
        <DialogTitle>{t('account:devices.confirmDelete')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('account:devices.deleteConfirmMessage', { deviceName: deviceToDelete?.remark || t('account:devices.unnamedDevice') })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>{t('common:common.cancel')}</Button>
          <Button onClick={handleDeleteConfirm} color="error">
            {t('common:common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
