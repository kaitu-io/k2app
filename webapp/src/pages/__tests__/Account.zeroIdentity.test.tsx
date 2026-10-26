/**
 * 【我的】零身份设备账号（设备账号 spec §4.4 / §4.6 / §4.6.1）：
 * - 不提供普通退出：换成【其他设备安装】和「登录其他账号」。
 * - 有会员时「登录其他账号」先弹 MUI Dialog 引导设置邮箱，不直接打开登录框。
 * - 显示可复制的用户 ID，隐藏邮箱相关项。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, within } from '@testing-library/react';
import { render } from '../../test/utils/render';
import i18n, { i18nPromise, changeLanguage } from '../../i18n/i18n';

const m = vi.hoisted(() => ({ openLogin: vi.fn(), navigate: vi.fn() }));

vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return { ...actual, useAuth: vi.fn() };
});
vi.mock('../../hooks/useUser', () => ({ useUser: vi.fn() }));
vi.mock('../../hooks/useAppLinks', () => ({ useAppLinks: () => ({ links: { walletUrl: '' } }) }));
vi.mock('../../hooks/usePrivateNodes', () => ({ usePrivateNodes: () => ({ nodes: [] }) }));
vi.mock('../../services/cloud-api', () => ({ cloudApi: { request: vi.fn(), post: vi.fn() } }));
vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return { ...actual, useLoginDialogStore: { getState: () => ({ open: m.openLogin }) } };
});
vi.mock('../../components/VersionItem', () => ({ default: () => <div data-testid="version-item" /> }));
vi.mock('../../components/BetaChannelToggle', () => ({ default: () => null }));
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<any>('react-router-dom');
  return { ...actual, useNavigate: () => m.navigate };
});

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';

const UUID = '7d1c2c1e-1111-4222-8333-944455556666';

function setUser(user: any, isMembership: boolean) {
  vi.mocked(useUser).mockReturnValue({
    user, loading: false, isMembership, isExpired: !isMembership, fetchUser: vi.fn(),
  } as any);
}

describe('Account — 零身份设备账号', () => {
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks 清掉了 setup.ts 的 getComputedStyle 实现；MUI Dialog 要读 paddingRight。
    const styles: Record<string, string> = { visibility: 'visible', display: 'block', paddingRight: '0px', overflow: 'visible' };
    (window.getComputedStyle as any).mockImplementation(() =>
      new Proxy(styles, {
        get(target, prop) {
          if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
          return typeof prop === 'string' ? target[prop] || '' : undefined;
        },
      }),
    );
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
    (window as any)._platform = {
      os: 'ios', version: '1.0.0', keychainIdentity: true,
      openExternal: vi.fn(), writeClipboard: vi.fn().mockResolvedValue(undefined),
    };
    vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
    setUser({ uuid: UUID, expiredAt: 4102444800, loginIdentifies: [], deviceCount: 1 }, true);
  });
  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
  });

  it('没有退出 / 切换账号按钮，换成【其他设备安装】和「登录其他账号」', () => {
    render(<Account />);
    expect(screen.queryByText(i18n.t('account:account.switchAccount'))).toBeNull();
    expect(screen.getByTestId('zero-identity-device-install')).toBeInTheDocument();
    expect(screen.getByText(i18n.t('account:account.loginOtherAccount'))).toBeInTheDocument();
  });

  it('显示可复制的用户 ID，隐藏邮箱相关项', () => {
    render(<Account />);
    expect(screen.getByText(UUID)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(i18n.t('account:account.copyUserId')));
    expect((window as any)._platform.writeClipboard).toHaveBeenCalledWith(UUID);
    expect(screen.queryByText(i18n.t('account:account.setEmail'))).toBeNull();
    expect(screen.queryByText(i18n.t('account:account.loginEmail'))).toBeNull();
    expect(screen.queryByText(i18n.t('account:account.multiDeviceWarning'))).toBeNull();
  });

  it('有会员：点「登录其他账号」先弹 Dialog，不直接打开登录框', () => {
    render(<Account />);
    fireEvent.click(screen.getByText(i18n.t('account:account.loginOtherAccount')));
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByText(i18n.t('account:account.loginOtherWarning'))).toBeInTheDocument();
    expect(m.openLogin).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByText(i18n.t('account:account.loginAnyway')));
    expect(m.openLogin).toHaveBeenCalledTimes(1);
  });

  it('Dialog「去设置邮箱」进【其他设备安装】', () => {
    render(<Account />);
    fireEvent.click(screen.getByText(i18n.t('account:account.loginOtherAccount')));
    fireEvent.click(within(screen.getByRole('dialog')).getByText(i18n.t('account:account.goSetEmail')));
    expect(m.navigate).toHaveBeenCalledWith('/device-install');
    expect(m.openLogin).not.toHaveBeenCalled();
  });

  it('无会员：直接打开登录框', () => {
    setUser({ uuid: UUID, expiredAt: 0, loginIdentifies: [], deviceCount: 1 }, false);
    render(<Account />);
    fireEvent.click(screen.getByText(i18n.t('account:account.loginOtherAccount')));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(m.openLogin).toHaveBeenCalledTimes(1);
  });

  it('有邮箱身份：保持原样（切换账号按钮在，无用户 ID 行）', () => {
    setUser({ uuid: UUID, expiredAt: 4102444800, loginIdentifies: [{ type: 'email', value: 'a@b.com' }] }, true);
    render(<Account />);
    expect(screen.getByText(i18n.t('account:account.switchAccount'))).toBeInTheDocument();
    expect(screen.queryByText(UUID)).toBeNull();
    expect(screen.queryByText(i18n.t('account:account.loginOtherAccount'))).toBeNull();
  });
});
