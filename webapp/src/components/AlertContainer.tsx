/**
 * AlertContainer - 全局 Toast 通知容器
 *
 * 渲染 Snackbar 组件，订阅 alert store
 */

import { Snackbar, Alert } from '@mui/material';
import { useAlertState } from '../stores';

export function AlertContainer() {
  const { open, message, severity, duration, hideAlert } = useAlertState();

  return (
    <Snackbar
      open={open}
      autoHideDuration={duration}
      onClose={hideAlert}
      anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
    >
      <Alert severity={severity} sx={{ width: '100%' }} onClose={hideAlert}>
        {message}
      </Alert>
    </Snackbar>
  );
}

export default AlertContainer;
