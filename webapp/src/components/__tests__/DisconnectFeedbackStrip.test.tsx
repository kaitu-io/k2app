/**
 * DisconnectFeedbackStrip Tests
 *
 * Non-blocking bottom-anchored rating strip. Replaces the old modal Dialog.
 * Run: cd webapp && npx vitest run src/components/__tests__/DisconnectFeedbackStrip.test.tsx
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const translations: Record<string, string> = {
        'feedback:feedback.disconnectFeedback.title': '本次连接体验如何？',
        'feedback:feedback.disconnectFeedback.detailTitle': '遇到了什么问题？',
        'feedback:feedback.disconnectFeedback.submit': '提交',
        'feedback:feedback.disconnectFeedback.tags.slow': '速度慢',
        'feedback:feedback.disconnectFeedback.tags.cantConnect': '连不上',
        'feedback:feedback.disconnectFeedback.tags.frequentDrops': '经常断开',
        'feedback:feedback.disconnectFeedback.tags.contentBlocked': '视频或网页打不开',
        'feedback:feedback.disconnectFeedback.tags.other': '其他',
      };
      return translations[key] || key;
    },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

const mockClearPendingFeedback = vi.fn();
let connectionState = {
  pendingFeedback: false,
  lastConnectionInfo: null as null | typeof mockConnectionInfo,
  clearPendingFeedback: mockClearPendingFeedback,
};
let vpnState: 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'disconnecting' | 'serviceDown' = 'idle';

vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (selector: (s: typeof connectionState) => unknown) => selector(connectionState),
}));

vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachineStore: (selector: (s: { state: typeof vpnState }) => unknown) => selector({ state: vpnState }),
}));

const mockPost = vi.fn().mockResolvedValue({ code: 0 });
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: (...args: unknown[]) => mockPost(...args) },
}));

vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: () => Promise.resolve('test-udid'),
}));

vi.mock('../../services/network-env', () => ({
  refreshNetworkEnv: () => Promise.resolve({ publicIP: '1.2.3.4' }),
}));

vi.mock('../../utils/uuid', () => ({
  randomUUID: () => 'test-uuid-fixed',
}));

import { DisconnectFeedbackStrip } from '../DisconnectFeedbackStrip';

const mockConnectionInfo = {
  domain: 'test.example.com',
  name: 'Tokyo-01',
  country: 'JP',
  source: 'cloud' as const,
  durationSec: 120,
  ruleMode: 'global',
  os: 'macos',
  appVersion: '0.4.0',
  commit: 'abc1234',
};

function setPending(pending: boolean, info = mockConnectionInfo) {
  connectionState = {
    ...connectionState,
    pendingFeedback: pending,
    lastConnectionInfo: pending ? info : null,
  };
}

function setVpnState(s: typeof vpnState) {
  vpnState = s;
}

function clickStar(value: number) {
  const stars = screen.getAllByRole('radio');
  const target = stars.find((el) => el.getAttribute('value') === String(value));
  if (!target) throw new Error(`star ${value} not found`);
  fireEvent.click(target);
}

function getRatingCalls() {
  return mockPost.mock.calls.filter((c) => c[0] === '/api/user/connection-rating');
}
function getTicketCalls() {
  return mockPost.mock.calls.filter((c) => c[0] === '/api/user/ticket');
}
function getUploadCalls() {
  return mockPost.mock.calls.filter((c) => c[0] === '/api/user/feedback-notify');
}

describe('DisconnectFeedbackStrip', () => {
  beforeEach(() => {
    mockPost.mockClear();
    mockPost.mockResolvedValue({ code: 0 });
    mockClearPendingFeedback.mockClear();
    connectionState = {
      pendingFeedback: false,
      lastConnectionInfo: null,
      clearPendingFeedback: mockClearPendingFeedback,
    };
    vpnState = 'idle';
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('renders with aria-hidden when pendingFeedback is false', () => {
    render(<DisconnectFeedbackStrip />);
    const strip = screen.getByTestId('disconnect-feedback-strip');
    expect(strip.getAttribute('aria-hidden')).toBe('true');
  });

  it('becomes visible when pendingFeedback flips true', () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    const strip = screen.getByTestId('disconnect-feedback-strip');
    expect(strip.getAttribute('aria-hidden')).toBe('false');
    expect(screen.getByText('本次连接体验如何？')).toBeTruthy();
  });

  it('tapping 5★ submits good and clears pendingFeedback', async () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(5);
    await Promise.resolve();
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'good' });
    expect(mockClearPendingFeedback).toHaveBeenCalled();
  });

  it('tapping 3★ submits bad (no tags, no ticket)', async () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(3);
    await Promise.resolve();
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'bad' });
    expect(getTicketCalls().length).toBe(0);
  });

  it('tapping 1★ enters CHIPS state and stops countdown', () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(1);
    expect(screen.getByText('遇到了什么问题？')).toBeTruthy();
    expect(screen.getByText('提交')).toBeTruthy();
    expect(getRatingCalls().length).toBe(0);
  });

  it('CHIPS: tapping 4★ upgrades rating, submits good with no tags', async () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(1);
    clickStar(4);
    await Promise.resolve();
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'good' });
    expect(getTicketCalls().length).toBe(0);
  });

  it('CHIPS: submit fires bad with selected tags + ticket + feedback-notify', async () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(2);
    fireEvent.click(screen.getByText('速度慢'));
    fireEvent.click(screen.getByText('视频或网页打不开'));
    fireEvent.click(screen.getByText('提交'));
    // Allow async submit chain to resolve.
    for (let i = 0; i < 6; i++) await Promise.resolve();
    const tickets = getTicketCalls();
    expect(tickets.length).toBe(1);
    expect(tickets[0][1].content).toContain('速度慢');
    expect(tickets[0][1].content).toContain('视频或网页打不开');
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1].rating).toBe('bad');
    expect(getUploadCalls().length).toBe(1);
  });

  it('countdown timeout defaults to good', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'performance', 'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout'] });
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    await act(async () => {
      vi.advanceTimersByTime(5100);
    });
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'good' });
  });

  it('visibilitychange pauses countdown; resume fires after remaining duration', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'performance', 'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout'] });
    setPending(true);
    render(<DisconnectFeedbackStrip />);

    // Drain 2s of the 5s window.
    await act(async () => { vi.advanceTimersByTime(2000); });
    expect(getRatingCalls().length).toBe(0);

    // Go to background. 10s passes while hidden — should not fire.
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      vi.advanceTimersByTime(10000);
    });
    expect(getRatingCalls().length).toBe(0);

    // Resume. Remaining ~3s should fire submit.
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      vi.advanceTimersByTime(3100);
    });
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'good' });
  });

  it('reconnect during COUNTDOWN defaults to good', async () => {
    setPending(true);
    const { rerender } = render(<DisconnectFeedbackStrip />);
    setVpnState('connecting');
    rerender(<DisconnectFeedbackStrip />);
    await Promise.resolve();
    const ratings = getRatingCalls();
    expect(ratings.length).toBe(1);
    expect(ratings[0][1]).toMatchObject({ rating: 'good' });
  });

  it('reconnect during CHIPS submits bad with current tags', async () => {
    setPending(true);
    const { rerender } = render(<DisconnectFeedbackStrip />);
    clickStar(1);
    fireEvent.click(screen.getByText('经常断开'));
    setVpnState('connecting');
    rerender(<DisconnectFeedbackStrip />);
    for (let i = 0; i < 6; i++) await Promise.resolve();
    const tickets = getTicketCalls();
    expect(tickets.length).toBe(1);
    expect(tickets[0][1].content).toContain('经常断开');
  });

  it('submit guard: rapid double-tap on 5★ fires submit exactly once', async () => {
    setPending(true);
    render(<DisconnectFeedbackStrip />);
    clickStar(5);
    clickStar(5);
    await Promise.resolve();
    expect(getRatingCalls().length).toBe(1);
  });

  it('new pendingFeedback cycle resets stars and tags', async () => {
    setPending(true);
    const { rerender } = render(<DisconnectFeedbackStrip />);
    clickStar(1);
    fireEvent.click(screen.getByText('速度慢'));
    // simulate dismissal then a fresh cycle with different info
    setPending(false);
    rerender(<DisconnectFeedbackStrip />);
    const nextInfo = { ...mockConnectionInfo, domain: 'tokyo2.example.com' };
    connectionState = {
      ...connectionState,
      pendingFeedback: true,
      lastConnectionInfo: nextInfo,
    };
    rerender(<DisconnectFeedbackStrip />);
    await Promise.resolve();
    // chips should no longer be visible (back to countdown state with 0 stars)
    expect(screen.queryByText('遇到了什么问题？')).toBeNull();
  });
});
