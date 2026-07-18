/**
 * RouterConnectionCard — Router tab 第一段:路由器隧道状态大按钮。
 * 主语是路由器(远端 k2r),数据来自 router.store 轮询,不碰本机 VPN 状态机。
 * 连接前若本机 VPN 已连接,由父层 RouterPage 的互斥守卫拦截(真实 Dialog 见 B9)。
 *
 * router 可能为 null 即便 phase==='online'(轮询/unbind 竞态,B7 review 结论)——
 * 本组件的每个 router 字段引用都必须 null-guard,不可假设 phase 蕴含 router 存在。
 */
import { Card, CardContent, Typography, Button, Stack, Chip, CircularProgress, Alert } from '@mui/material';
import { Router as RouterIcon } from '@mui/icons-material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useRouterStore } from '../stores/router.store';
import { getErrorMessage } from '../utils/errorCode';

export function RouterConnectionCard({ onBeforeConnect }: { onBeforeConnect?: () => Promise<boolean> }) {
  const { t } = useTranslation();
  const router = useRouterStore((s) => s.router);
  const status = useRouterStore((s) => s.status);
  const connectRouter = useRouterStore((s) => s.connectRouter);
  const disconnectRouter = useRouterStore((s) => s.disconnectRouter);
  const [busy, setBusy] = useState(false);

  const connected = status?.state === 'connected';
  // k2r 的 /api/core status 直出 engine.Status 原始形态(不经过本机 daemon 的
  // transformStatus 归一化)，错误字段键是 error，不是本机约定的 lastError——
  // 见 services/status-transform.ts:25 `raw.error`。EngineError({code, message})
  // 复用既有 code→i18n 映射(utils/errorCode.ts getErrorMessage)，禁止直显 message。
  const lastError = (status as { error?: { code?: number; message?: string } } | null)?.error;

  const handleToggle = async () => {
    if (busy) return;
    if (!connected && onBeforeConnect && !(await onBeforeConnect())) return;
    setBusy(true);
    try {
      await (connected ? disconnectRouter() : connectRouter());
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card data-testid="router-connection-card">
      <CardContent>
        <Stack direction="row" alignItems="center" spacing={1}>
          <RouterIcon color={connected ? 'success' : 'disabled'} />
          <Typography variant="h6" sx={{ fontSize: '1rem', fontWeight: 700, flex: 1 }}>
            {router?.name || t('router:router.title')}
          </Typography>
          <Chip
            size="small"
            data-testid="router-conn-state"
            color={connected ? 'success' : 'default'}
            label={t(connected ? 'router:router.connected' : 'router:router.disconnected')}
          />
        </Stack>
        <Button
          fullWidth
          variant="contained"
          color={connected ? 'error' : 'primary'}
          sx={{ mt: 2 }}
          data-testid="router-toggle"
          disabled={busy}
          onClick={() => void handleToggle()}
          startIcon={busy ? <CircularProgress size={16} /> : undefined}
        >
          {t(connected ? 'router:router.disconnect' : 'router:router.connect')}
        </Button>
        {lastError?.code ? (
          <Alert severity="error" data-testid="router-conn-error" sx={{ mt: 1 }}>
            {getErrorMessage(lastError.code, lastError.message, t)}
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  );
}
