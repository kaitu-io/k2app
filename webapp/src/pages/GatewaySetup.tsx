/**
 * GatewaySetup — 路由器面板「设置」页（k2r gateway, Plan 5b）
 *
 * 仅在 `window._platform.platformType === 'gateway'` 时挂载到 `/setup`。
 * 用户把在开途 App 或网页端 mint 出的 `k2subs://` 路由器连接地址粘贴进来，
 * 提交给本地 k2r 的 set-credential action（经 bridge 层 window._k2.run），
 * 完成专属线路连接。
 */

import { useState } from 'react';
import { Box, TextField, Button, Typography, Alert } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { gatewaySetCredential } from '../services/gateway-core';

export default function GatewaySetup() {
  const { t } = useTranslation();
  const [url, setUrl] = useState('');
  const [status, setStatus] = useState<'idle' | 'ok' | 'err'>('idle');

  const handleSubmit = async () => {
    const r = await gatewaySetCredential(url.trim());
    setStatus(r.code === 0 ? 'ok' : 'err');
  };

  return (
    <Box sx={{ p: 2, maxWidth: 560 }}>
      <Typography variant="h6">
        {t('privateNode:privateNode.setup.title')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
        {t('privateNode:privateNode.setup.hint')}
      </Typography>
      <TextField
        inputProps={{ 'data-testid': 'setup-url-input' }}
        fullWidth
        sx={{ mt: 2 }}
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="k2subs://..."
      />
      <Button
        data-testid="setup-submit"
        variant="contained"
        sx={{ mt: 2 }}
        onClick={handleSubmit}
      >
        {t('privateNode:privateNode.setup.connect')}
      </Button>
      {status === 'ok' && (
        <Alert data-testid="setup-ok" severity="success" sx={{ mt: 2 }}>
          {t('privateNode:privateNode.setup.ok')}
        </Alert>
      )}
      {status === 'err' && (
        <Alert data-testid="setup-err" severity="error" sx={{ mt: 2 }}>
          {t('privateNode:privateNode.setup.err')}
        </Alert>
      )}
    </Box>
  );
}
