/**
 * RouterTakeoverBanner — Dashboard 顶部提示:本机 VPN 未连接,但当前网络的
 * 路由器(k2r)已接管流量。消除"未连接=未保护"的误读(spec §6.3)。
 * 本机一旦连接(双连或用户改走本机),横幅让位——避免和本机连接状态打架。
 */
import { Alert, Box } from '@mui/material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useRouterStore, isRouterTakeover } from '../stores/router.store';
import { useVpnStore } from '../stores/vpn.store';

export function RouterTakeoverBanner() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const takeover = useRouterStore((s) => isRouterTakeover(s));
  const localState = useVpnStore((s) => s.status?.state);
  if (!takeover || localState === 'connected') return null;
  return (
    <Box sx={{ px: 2, pt: 1 }}>
      <Alert
        severity="success"
        data-testid="router-takeover-banner"
        onClick={() => navigate('/router')}
        sx={{ cursor: 'pointer' }}
      >
        {t('router:takeover.banner')}
      </Alert>
    </Box>
  );
}
