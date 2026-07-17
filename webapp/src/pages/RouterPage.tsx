/**
 * RouterPage — 顶层 Router tab(与 Dashboard 平级,keep-alive)。
 * 三段:连接卡 / LAN 设备 / 路由器设置。出现条件:routerStore.phase !== 'none'
 * (由 Layout.tsx 的动态 tab 列表控制,本组件本身在 phase==='none' 时防御性渲染 null)。
 * 主语永远是路由器;本机 VPN 归 Dashboard。
 *
 * 互斥守卫(本机 VPN 与路由器接管互斥)在此先给出直通占位实现——真实拦截 Dialog
 * 是 B9 的范围(独立组件 RouterExclusionDialog,不在 B8 文件清单内)。B9 落地后
 * 把 useExclusionGuard 换成对该组件的导入。
 */
import { useEffect } from 'react';
import { Box, Typography, Button, Card, CardContent, Stack } from '@mui/material';
import { WifiOff as OfflineIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useRouterStore } from '../stores/router.store';
import { useVpnStore } from '../stores/vpn.store';
import { RouterConnectionCard } from '../components/RouterConnectionCard';
import { RouterSetupCard } from '../components/RouterSetupCard';
import RouterDevicesSection from './RouterDevices';

/** 占位互斥守卫:直通放行,不拦截、不弹窗。B9 用 RouterExclusionDialog 替换。 */
function useExclusionGuard() {
  return {
    guard: async (_localConnected?: boolean) => true,
    confirmUnbind: (fn: () => void) => fn(),
  };
}

export default function RouterPage() {
  const { t } = useTranslation();
  const phase = useRouterStore((s) => s.phase);
  const router = useRouterStore((s) => s.router);
  const runDiscovery = useRouterStore((s) => s.runDiscovery);
  const startPolling = useRouterStore((s) => s.startPolling);
  const stopPolling = useRouterStore((s) => s.stopPolling);
  const unbindRouter = useRouterStore((s) => s.unbindRouter);
  const localState = useVpnStore((s) => s.status?.state);
  const exclusion = useExclusionGuard();

  useEffect(() => {
    startPolling();
    return () => stopPolling();
  }, [startPolling, stopPolling]);

  // Layout's tabPages only mounts this route once hasRouter (phase !== 'none'),
  // so this branch is defensive only (e.g. a stale render during unbind's
  // phase flip) — an empty fragment keeps the return type JSX.Element, matching
  // Layout's TabPageConfig contract (LazyExoticComponent<() => JSX.Element>).
  if (phase === 'none') {
    return <></>;
  }

  if (phase === 'unconfigured') {
    return (
      <Box sx={{ p: 2 }}>
        <RouterSetupCard />
      </Box>
    );
  }

  if (phase === 'offline') {
    return (
      <Box sx={{ p: 2, textAlign: 'center' }} data-testid="router-offline">
        <OfflineIcon color="disabled" sx={{ fontSize: 48, mt: 4 }} />
        <Typography color="text.secondary" sx={{ mt: 1 }}>
          {t('router:router.offline', { name: router?.name || '' })}
        </Typography>
        <Button sx={{ mt: 2 }} onClick={() => void runDiscovery()} data-testid="router-retry">
          {t('router:router.retry')}
        </Button>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 2 }}>
      <Stack spacing={2}>
        <RouterConnectionCard
          onBeforeConnect={() => exclusion.guard(localState === 'connected')}
        />
        <RouterDevicesSection />
        <Card data-testid="router-settings-section">
          <CardContent>
            <Typography variant="h6" sx={{ fontSize: '1rem', fontWeight: 700 }}>
              {t('router:settings.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
              {t('router:settings.version', { version: router?.version || '' })}
            </Typography>
            <Button
              color="error"
              sx={{ mt: 1 }}
              data-testid="router-unbind"
              onClick={() => exclusion.confirmUnbind(() => void unbindRouter())}
            >
              {t('router:settings.unbind')}
            </Button>
          </CardContent>
        </Card>
      </Stack>
    </Box>
  );
}
