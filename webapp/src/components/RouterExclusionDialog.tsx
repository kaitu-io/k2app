/**
 * RouterExclusionDialog — 双连互斥强提醒。
 * 只在「双连即将成立」时弹(spec §6.3),不在 tab 切换时骚扰:
 *   router-connect:  Router tab 点连接 && 本机 VPN connected
 *   dashboard-connect: Dashboard 点连接 && isRouterTakeover
 * 宪法:Capacitor WebView 吞 window.confirm——必须 MUI Dialog。
 *
 * 同一个 controller 还承载 unbind(解绑路由器)的二次确认——之前 RouterPage
 * 的占位实现直接调用回调、完全没有确认;这里补上真实的 MUI 确认弹窗
 * (同样是为了绕开被 WebView 吞掉的 window.confirm)。
 */
import {
  Dialog, DialogTitle, DialogContent, DialogContentText, DialogActions, Button,
} from '@mui/material';
import { useRef, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useConnectionStore } from '../stores/connection.store';
import { useRouterStore, routerSlots } from '../stores/router.store';

export type ExclusionContext = 'router-connect' | 'dashboard-connect';

export interface ExclusionController {
  open: boolean;
  context: ExclusionContext;
  guard: (shouldWarn: boolean) => Promise<boolean>;
  confirmUnbind: (fn: () => void) => void;
  resolveChoice: (choice: 'proceed' | 'keep' | 'cancel') => void;
  unbindPending: (() => void) | null;
  clearUnbind: () => void;
}

export function useExclusionGuard(context: ExclusionContext): ExclusionController {
  const [open, setOpen] = useState(false);
  const [unbindPending, setUnbindPending] = useState<(() => void) | null>(null);
  const resolver = useRef<((v: boolean) => void) | null>(null);
  const disconnect = useConnectionStore((s) => s.disconnect);
  const disconnectRouter = useRouterStore((s) => s.disconnectRouter);

  const guard = useCallback(
    (shouldWarn: boolean): Promise<boolean> => {
      if (!shouldWarn) return Promise.resolve(true);
      setOpen(true);
      return new Promise((resolve) => {
        resolver.current = resolve;
      });
    },
    [],
  );

  const resolveChoice = useCallback(
    (choice: 'proceed' | 'keep' | 'cancel') => {
      setOpen(false);
      const resolve = resolver.current;
      resolver.current = null;
      if (!resolve) return;
      if (choice === 'cancel') {
        resolve(false);
        return;
      }
      if (choice === 'proceed') {
        // proceed = 断开「另一边」再继续
        if (context === 'router-connect') void disconnect();
        else void disconnectRouter();
      }
      resolve(true); // proceed 与 keep 都继续本侧连接
    },
    [context, disconnect, disconnectRouter],
  );

  return {
    open,
    context,
    guard,
    resolveChoice,
    unbindPending,
    confirmUnbind: (fn) => setUnbindPending(() => fn),
    clearUnbind: () => setUnbindPending(null),
  };
}

export function RouterExclusionDialog({ controller }: { controller: ExclusionController }) {
  const { t } = useTranslation();
  const c = controller;
  const isRouterSide = c.context === 'router-connect';
  // Enterprise multi-slot router: unbinding takes ALL customer lines offline
  // and recovery needs the operator — the confirmation copy must say so.
  const isEnterprise = useRouterStore((s) => routerSlots(s) !== null);
  return (
    <>
      <Dialog open={c.open} onClose={() => c.resolveChoice('cancel')} data-testid="router-exclusion-dialog">
        <DialogTitle>{t('router:exclusion.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t(isRouterSide ? 'router:exclusion.bodyRouter' : 'router:exclusion.bodyDashboard')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button data-testid="exclusion-keep" onClick={() => c.resolveChoice('keep')}>
            {t('router:exclusion.keep')}
          </Button>
          <Button data-testid="exclusion-proceed" variant="contained" onClick={() => c.resolveChoice('proceed')}>
            {t(isRouterSide ? 'router:exclusion.proceedRouter' : 'router:exclusion.proceedDashboard')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={c.unbindPending !== null} onClose={c.clearUnbind} data-testid="router-unbind-dialog">
        <DialogTitle>{t('router:settings.unbindConfirmTitle')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t(isEnterprise ? 'router:slots.unbindEnterpriseBody' : 'router:settings.unbindConfirmBody')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={c.clearUnbind}>{t('router:settings.unbindCancel')}</Button>
          <Button
            color="error"
            data-testid="router-unbind-confirm"
            onClick={() => {
              c.unbindPending?.();
              c.clearUnbind();
            }}
          >
            {t('router:settings.unbindConfirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
