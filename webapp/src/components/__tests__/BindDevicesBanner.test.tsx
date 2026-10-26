/**
 * 全局绑定横幅（设备账号 spec §4.3）：零身份 + 有会员 + Keychain 新壳才显示，常驻、不可关闭，
 * 点击进【其他设备安装】。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import i18n, { i18nPromise, changeLanguage } from '../../i18n/i18n';

let mockUser: any = null;
vi.mock('../../hooks/useUser', () => ({ useUser: () => ({ user: mockUser, fetchUser: vi.fn() }) }));

import BindDevicesBanner from '../BindDevicesBanner';

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname}</div>;
}

function renderBanner() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/']}>
        <BindDevicesBanner />
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  );
}

const future = () => Math.floor(Date.now() / 1000) + 5 * 86400;

describe('BindDevicesBanner', () => {
  let originalPlatform: any;
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    originalPlatform = window._platform;
    (window as any)._platform = { keychainIdentity: true };
    mockUser = { loginIdentifies: [], expiredAt: future(), maxDevice: 5 };
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  it('zero identity + active membership renders and navigates to /device-install', () => {
    renderBanner();
    const banner = screen.getByTestId('bind-devices-banner');
    expect(banner.textContent).toContain('绑定邮箱或 Apple 登录，就能在 5 台设备上用');
    expect(screen.queryByRole('button', { name: /close|关闭/i })).toBeNull();
    fireEvent.click(banner);
    expect(screen.getByTestId('location').textContent).toBe('/device-install');
  });

  it('absent loginIdentifies counts as zero identity', () => {
    mockUser = { expiredAt: future(), maxDevice: 0 };
    renderBanner();
    expect(screen.getByTestId('bind-devices-banner').textContent).toContain('5 台');
  });

  it('bound identity → not rendered', () => {
    mockUser = { ...mockUser, loginIdentifies: [{ type: 'email', value: 'a@b.com' }] };
    renderBanner();
    expect(screen.queryByTestId('bind-devices-banner')).toBeNull();
  });

  it('no membership (expired) → not rendered', () => {
    mockUser = { ...mockUser, expiredAt: Math.floor(Date.now() / 1000) - 10 };
    renderBanner();
    expect(screen.queryByTestId('bind-devices-banner')).toBeNull();
  });

  it('no user → not rendered', () => {
    mockUser = null;
    renderBanner();
    expect(screen.queryByTestId('bind-devices-banner')).toBeNull();
  });

  it('keychainIdentity undefined (old shell / other platforms) → not rendered', () => {
    (window as any)._platform = {};
    renderBanner();
    expect(screen.queryByTestId('bind-devices-banner')).toBeNull();
  });
});
