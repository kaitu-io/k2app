/**
 * RouterPage — 顶层 Router tab(与 Dashboard 平级,keep-alive)。
 * 三段:连接卡 / LAN 设备 / 路由器设置。出现条件:routerStore.phase !== 'none'
 * (由 Layout.tsx 的动态 tab 列表控制,本组件本身在 phase==='none' 时防御性渲染 null)。
 * 主语永远是路由器;本机 VPN 归 Dashboard。
 *
 * 互斥守卫(本机 VPN 与路由器接管互斥)+ unbind 二次确认均来自 B9 的
 * RouterExclusionDialog(components/RouterExclusionDialog.tsx)——替换了 B8 的
 * 直通占位实现(guard 恒真、confirmUnbind 直接调用回调、无 Dialog)。
 */
import { useEffect } from 'react';
import { Box, Typography, Button, Card, CardContent, Stack } from '@mui/material';
import { WifiOff as OfflineIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';
import { useRouterStore, routerSlots } from '../stores/router.store';
import { useVpnStore } from '../stores/vpn.store';
import { RouterConnectionCard } from '../components/RouterConnectionCard';
import { RouterSetupCard } from '../components/RouterSetupCard';
import { useExclusionGuard, RouterExclusionDialog } from '../components/RouterExclusionDialog';
import RouterSlotList from '../components/RouterSlotList';
import RouterDevicesSection from './RouterDevices';

export default function RouterPage() {
  const { t } = useTranslation();
  const phase = useRouterStore((s) => s.phase);
  const router = useRouterStore((s) => s.router);
  const runDiscovery = useRouterStore((s) => s.runDiscovery);
  const startPolling = useRouterStore((s) => s.startPolling);
  const stopPolling = useRouterStore((s) => s.stopPolling);
  const unbindRouter = useRouterStore((s) => s.unbindRouter);
  const slots = useRouterStore(routerSlots);
  const unauthorized = useRouterStore((s) => s.unauthorized);
  const localState = useVpnStore((s) => s.status?.state);
  const exclusion = useExclusionGuard('router-connect');
  const navigate = useNavigate();
  const location = useLocation();

  // Layout keep-alive tabs are hidden by CSS (visibility), never unmounted,
  // once visited — so a mount-only effect with unmount cleanup never actually
  // stops the 2s poll after the first Router-tab visit (I2 fix). Gate on the
  // route actually being active instead, using the same signal Layout uses
  // to compute isActive (location.pathname === tab.path). This both starts
  // polling when the tab becomes active and stops it the moment it doesn't
  // (tab switch, not just true unmount) — spec §6.1 "tab 可见时才轮询，隐藏即停".
  const isActive = location.pathname === '/router';
  useEffect(() => {
    if (!isActive) {
      stopPolling();
      return;
    }
    startPolling();
    return () => stopPolling();
  }, [isActive, startPolling, stopPolling]);

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
        {unauthorized && !slots ? (
          // k2r rejects this account's controlKey — the router belongs to
          // another account (typical: enterprise router + employee phone).
          // No connect card: there is nothing this account can control.
          <Card data-testid="router-managed-by-account">
            <CardContent>
              <Typography color="text.secondary">
                {t('router:slots.managedByAccount')}
              </Typography>
            </CardContent>
          </Card>
        ) : slots ? (
          // Enterprise multi-slot form: the tunnel lifecycle is owned by the
          // operator's binding manifest — no consumer up/down card.
          <RouterSlotList slots={slots} />
        ) : (
          <RouterConnectionCard
            onBeforeConnect={() => exclusion.guard(localState === 'connected')}
          />
        )}
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
              onClick={() =>
                exclusion.confirmUnbind(() => {
                  // Post-unbind: phase→'none' removes the /router tab entity
                  // and RouterPage itself renders <></> (see the phase==='none'
                  // branch above) while the route is still /router — a blank
                  // page. Navigate back to Dashboard once unbind succeeds (M4).
                  void unbindRouter().then((ok) => {
                    if (ok) navigate('/');
                  });
                })
              }
            >
              {t('router:settings.unbind')}
            </Button>
          </CardContent>
        </Card>
      </Stack>
      <RouterExclusionDialog controller={exclusion} />
    </Box>
  );
}
