/**
 * BindDevicesBanner — 零身份设备账号的全局绑定提示（设备账号 spec 2026-10-20 §4.3）。
 *
 * 显示条件：Keychain 身份新壳（`_platform.keychainIdentity === true`）+ 当前用户没有任何
 * 登录身份（`loginIdentifies` 为空）+ 会员未过期（试用或付费，重装丢号最痛）。点击进
 * 【其他设备安装】绑定；绑定后条件不成立自动消失。在【其他设备安装】页本身不显示。
 *
 * 常驻细横幅：不可关闭、不弹窗、不阻断使用——强制绑定会撞 Apple 审核 5.1.1(v)。
 */

import { ButtonBase, Typography } from '@mui/material';
import { DevicesOutlined as DevicesIcon, ChevronRight as ChevronIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';
import { useUser } from '../hooks/useUser';

const DEVICE_INSTALL_PATH = '/device-install';
const DEFAULT_DEVICE_COUNT = 5;

/**
 * 外层只看平台 flag（boot 时定死，不会变）：非 Keychain 新壳根本不挂 useUser，
 * 免得 Layout 在桌面 / Android / 旧壳上平白多打一次 /api/user/info。
 */
export default function BindDevicesBanner() {
  if (window._platform?.keychainIdentity !== true) return null;
  return <ZeroIdentityBanner />;
}

function ZeroIdentityBanner() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { user } = useUser();

  const show =
    pathname !== DEVICE_INSTALL_PATH &&
    !!user &&
    !(user.loginIdentifies?.length > 0) &&
    user.expiredAt > Math.floor(Date.now() / 1000);
  if (!show) return null;

  const count = user.maxDevice && user.maxDevice > 0 ? user.maxDevice : DEFAULT_DEVICE_COUNT;

  return (
    <ButtonBase
      data-testid="bind-devices-banner"
      onClick={() => navigate(DEVICE_INSTALL_PATH)}
      sx={{
        width: '100%',
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 1.75,
        py: 1.5,
        minHeight: 44,
        boxSizing: 'border-box',
        borderRadius: 3,
        border: '1px solid',
        borderColor: 'primary.main',
        bgcolor: 'action.hover',
        textAlign: 'left',
      }}
    >
      <DevicesIcon sx={{ color: 'primary.main', fontSize: 22, flex: 'none' }} />
      <Typography sx={{ flex: 1, fontSize: 14, lineHeight: 1.4 }}>
        {t('account:account.bindBanner', { count })}
      </Typography>
      <ChevronIcon sx={{ color: 'text.secondary', fontSize: 18, flex: 'none' }} />
    </ButtonBase>
  );
}
