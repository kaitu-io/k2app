/**
 * RouterSetupCard — 首次配置引导(替代已退役的嵌入面板粘贴流程)。
 * 一键:mint k2subs 凭证 + 取 controlKey + 推送 set-credential(TOFU)。
 * router 可能为 null(见 B7 review 的竞态结论)——name 引用全部 null-guard。
 */
import { Card, CardContent, Typography, Button, Alert, CircularProgress } from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useRouterStore } from '../stores/router.store';

export function RouterSetupCard() {
  const { t } = useTranslation();
  const router = useRouterStore((s) => s.router);
  const setupRouter = useRouterStore((s) => s.setupRouter);
  const setupError = useRouterStore((s) => s.setupError);
  const [busy, setBusy] = useState(false);

  const handleSetup = async () => {
    setBusy(true);
    try {
      await setupRouter();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card data-testid="router-setup-card">
      <CardContent>
        <Typography variant="h6" sx={{ fontSize: '1rem', fontWeight: 700 }}>
          {t('router:setup.title', { name: router?.name || '' })}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          {t('router:setup.intro')}
        </Typography>
        {setupError && (
          <Alert severity="error" data-testid="router-setup-error" sx={{ mt: 2 }}>
            {t(`router:setup.error.${setupError}`)}
          </Alert>
        )}
        <Button
          fullWidth
          variant="contained"
          sx={{ mt: 2 }}
          data-testid="router-setup-submit"
          disabled={busy}
          onClick={() => void handleSetup()}
          startIcon={busy ? <CircularProgress size={16} /> : undefined}
        >
          {t('router:setup.submit')}
        </Button>
      </CardContent>
    </Card>
  );
}
