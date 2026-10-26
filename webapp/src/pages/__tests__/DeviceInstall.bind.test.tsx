/**
 * 【其他设备安装】绑定区（设计稿 ③，设备账号 spec §4.3）：
 * - Keychain 新壳 + 零身份：安装内容上方出现绑定区（说明、Apple bind、邮箱绑定表单）。
 * - 已有登录身份：没有绑定区，改为「在其他设备上用 … 登录即可」。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import i18n, { i18nPromise, changeLanguage } from '../../i18n/i18n';

const m = vi.hoisted(() => ({
  user: null as any,
  fetchUser: vi.fn(),
}));

vi.mock('../../hooks/useUser', () => ({
  useUser: () => ({ user: m.user, fetchUser: m.fetchUser, loading: false }),
}));
vi.mock('../../stores', async () => {
  const actual = await vi.importActual<any>('../../stores');
  return { ...actual, useAuthStore: (sel: any) => sel({ isAuthenticated: true }) };
});
vi.mock('../../services/cloud-api', () => ({ cloudApi: { get: vi.fn(), post: vi.fn() } }));
vi.mock('../../components/SocialSignInButtons', () => ({
  default: (props: any) => <div data-testid="social-buttons" data-mode={props.mode ?? 'login'} />,
}));
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('') } }));

import DeviceInstall from '../DeviceInstall';
import { cloudApi } from '../../services/cloud-api';

describe('DeviceInstall — 绑定区', () => {
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    (window as any)._platform = { os: 'ios', keychainIdentity: true };
    vi.mocked(cloudApi.get).mockResolvedValue({ code: 0, data: {} } as any);
  });
  afterEach(() => {
    delete (window as any)._platform;
  });

  it('零身份：显示绑定说明、Apple bind 按钮和邮箱绑定表单', () => {
    m.user = { uuid: 'u-1', loginIdentifies: [], maxDevice: 5, expiredAt: 4102444800 };
    render(<DeviceInstall />);

    expect(screen.getByText(i18n.t('account:deviceBind.title'))).toBeInTheDocument();
    expect(screen.getByText(i18n.t('account:deviceBind.description', { count: 5 }))).toBeInTheDocument();
    expect(screen.getByText(i18n.t('account:deviceBind.noCharge'))).toBeInTheDocument();
    expect(screen.getByTestId('bind-email-form')).toBeInTheDocument();
    expect(screen.getByTestId('social-buttons').getAttribute('data-mode')).toBe('bind');
    expect(screen.queryByText(/登录即可/)).toBeNull();
    // 安装内容仍在
    expect(screen.getByText(i18n.t('purchase:deviceInstall.title'))).toBeInTheDocument();
  });

  it('绑定区：邮箱已属别的账号（409001）提示换邮箱 / 联系客服，不引导去登录原账号', async () => {
    m.user = { uuid: 'u-1', loginIdentifies: [], maxDevice: 5, expiredAt: 4102444800 };
    vi.mocked(cloudApi.post).mockResolvedValue({ code: 409001, message: 'email already in use' } as any);
    render(<DeviceInstall />);

    const form = screen.getByTestId('bind-email-form');
    fireEvent.change(form.querySelector('input')!, { target: { value: 'taken@example.com' } });
    const send = Array.from(form.querySelectorAll('button')).find(
      (b) => b.textContent === i18n.t('auth:updateEmail.sendCode'),
    )!;
    fireEvent.click(send);

    await waitFor(() => expect(screen.getByText(i18n.t('account:deviceBind.emailConflict'))).toBeInTheDocument());
    expect(screen.queryByText(i18n.t('auth:updateEmail.emailAlreadyInUse'))).toBeNull();
  });

  it('设备数跟随套餐 maxDevice（与 MembershipBenefits 同源）', () => {
    m.user = { uuid: 'u-1', loginIdentifies: [], maxDevice: 3, expiredAt: 4102444800 };
    render(<DeviceInstall />);
    expect(screen.getByText(i18n.t('account:deviceBind.description', { count: 3 }))).toBeInTheDocument();
  });

  it('已绑定邮箱：无绑定区，显示用脱敏邮箱登录的提示', () => {
    m.user = { uuid: 'u-1', loginIdentifies: [{ type: 'email', value: 'alice@example.com' }], maxDevice: 5 };
    render(<DeviceInstall />);

    expect(screen.queryByTestId('bind-email-form')).toBeNull();
    expect(screen.queryByText(i18n.t('account:deviceBind.title'))).toBeNull();
    expect(
      screen.getByText(i18n.t('account:deviceBind.bound', { account: 'a***e@example.com', count: 5 })),
    ).toBeInTheDocument();
  });

  it('只绑了 Apple：提示用 Apple 登录', () => {
    m.user = { uuid: 'u-1', loginIdentifies: [{ type: 'apple', value: 'x' }], maxDevice: 5 };
    render(<DeviceInstall />);
    expect(screen.getByText(i18n.t('account:deviceBind.bound', { account: 'Apple', count: 5 }))).toBeInTheDocument();
  });

  it('非 Keychain 新壳：既不显示绑定区，也不改原页面', () => {
    (window as any)._platform = { os: 'android' };
    m.user = { uuid: 'u-1', loginIdentifies: [], maxDevice: 5 };
    render(<DeviceInstall />);
    expect(screen.queryByTestId('bind-email-form')).toBeNull();
    expect(screen.queryByText(/登录即可/)).toBeNull();
  });
});
