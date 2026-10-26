/**
 * 零身份已登录用户提工单（设备账号 spec §4.6.1）：表单要求填联系邮箱（只用于回复），
 * body 带 email；服务端 api_ticket.go 在账号无邮箱身份时用它。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import i18n from '../../i18n/i18n';

vi.mock('../../stores/auth.store', () => ({ useAuthStore: vi.fn() }));
vi.mock('../../hooks/useUser', () => ({ useUser: vi.fn() }));
vi.mock('../../services/cloud-api', () => ({ cloudApi: { post: vi.fn() } }));
vi.mock('../../services/device-udid', () => ({ getDeviceUdid: vi.fn().mockResolvedValue('test-udid') }));

import { useAuthStore } from '../../stores/auth.store';
import { useUser } from '../../hooks/useUser';
import { cloudApi } from '../../services/cloud-api';
import SubmitTicket from '../SubmitTicket';

const mockPost = vi.mocked(cloudApi.post);

const mockComputedStyle = () => ({
  width: '100px', height: '100px', boxSizing: 'content-box',
  paddingTop: '0px', paddingBottom: '0px', paddingLeft: '0px', paddingRight: '0px',
  borderTopWidth: '0px', borderBottomWidth: '0px', borderLeftWidth: '0px', borderRightWidth: '0px',
  overflow: 'visible', visibility: 'visible', display: 'block',
  minHeight: '0px', maxHeight: 'none',
  getPropertyValue: (_prop: string) => '',
});

function setUser(loginIdentifies: any[]) {
  vi.mocked(useAuthStore).mockImplementation((selector: any) => {
    const state = { isAuthenticated: true };
    return typeof selector === 'function' ? selector(state) : state;
  });
  vi.mocked(useUser).mockReturnValue({ user: { uuid: 'u-1', loginIdentifies } } as any);
}

const contentBox = () => screen.getByRole('textbox', { name: i18n.t('ticket:ticket.contentLabel') });
const emailBox = () => screen.queryByRole('textbox', { name: i18n.t('ticket:ticket.emailLabel') });
const submit = () => fireEvent.click(screen.getByRole('button', { name: new RegExp(i18n.t('ticket:ticket.submitButton')) }));

describe('SubmitTicket — 零身份已登录', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.getComputedStyle = vi.fn().mockImplementation(mockComputedStyle) as any;
    (window as any)._platform = { os: 'ios', version: '1.0.0' };
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0, data: { state: 'disconnected' } }) };
    mockPost.mockResolvedValue({ code: 0, data: null, message: 'ok' } as any);
    setUser([]);
  });
  afterEach(() => {
    delete (window as any)._platform;
    delete (window as any)._k2;
  });

  it('显示联系邮箱输入，空邮箱提交时提示 emailRequired，不发请求', async () => {
    render(<SubmitTicket />, { useMemoryRouter: true });
    expect(emailBox()).toBeInTheDocument();
    expect(screen.getByText(i18n.t('ticket:ticket.zeroIdentityHint'))).toBeInTheDocument();
    fireEvent.change(contentBox(), { target: { value: '连不上' } });
    submit();
    await waitFor(() => expect(screen.getByText(i18n.t('ticket:ticket.validation.emailRequired'))).toBeInTheDocument());
    expect(mockPost).not.toHaveBeenCalledWith('/api/user/ticket', expect.anything());
  });

  it('填了邮箱：body.email 等于输入值，且不标记为匿名', async () => {
    render(<SubmitTicket />, { useMemoryRouter: true });
    fireEvent.change(contentBox(), { target: { value: '连不上' } });
    fireEvent.change(emailBox()!, { target: { value: ' me@example.com ' } });
    submit();
    await waitFor(() => expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.anything()));
    const body = mockPost.mock.calls.find((c) => c[0] === '/api/user/ticket')![1] as any;
    expect(body.email).toBe('me@example.com');
    expect(body.source).toBeUndefined();
  });

  it('有邮箱身份：不显示邮箱输入，body 不带 email', async () => {
    setUser([{ type: 'email', value: 'a@b.com' }]);
    render(<SubmitTicket />, { useMemoryRouter: true });
    expect(emailBox()).toBeNull();
    fireEvent.change(contentBox(), { target: { value: '连不上' } });
    submit();
    await waitFor(() => expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.anything()));
    const body = mockPost.mock.calls.find((c) => c[0] === '/api/user/ticket')![1] as any;
    expect(body.email).toBeUndefined();
  });
});
