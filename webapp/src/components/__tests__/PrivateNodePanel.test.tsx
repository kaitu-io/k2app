import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '../../test/utils/render';
import { PrivateNodePanel } from '../PrivateNodePanel';
import type { PrivateNodeSubscriptionView } from '../../services/api-types';

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

function makeNode(overrides: Partial<PrivateNodeSubscriptionView>): PrivateNodeSubscriptionView {
  return {
    id: 1,
    status: 'active',
    isServiceable: true,
    region: 'ap-northeast-1',
    ipType: 'non_residential',
    trafficTotalBytes: 100 * 1024 ** 3,
    trafficUsedBytes: 40 * 1024 ** 3,
    purchasedAt: 1_700_000_000,
    expiresAt: 1_800_000_000,
    graceUntil: 0,
    suspendUntil: 0,
    planLabel: '专属节点测试',
    quotaExhausted: false,
    node: { ip: '1.2.3.4', region: 'ap-northeast-1' },
    ...overrides,
  };
}

describe('PrivateNodePanel', () => {
  it('active node: success chip, traffic bar with %, node IP', () => {
    render(<PrivateNodePanel node={makeNode({})} />);

    // status chip
    expect(screen.getByText('服务中')).toBeInTheDocument();
    // traffic progress bar present
    const bar = screen.getByTestId('private-node-traffic-bar');
    expect(bar).toBeInTheDocument();
    expect(bar.getAttribute('aria-valuenow')).toBe('40'); // 40/100 = 40%
    // node IP shown
    expect(screen.getByText('1.2.3.4')).toBeInTheDocument();
    // ipType translated
    expect(screen.getByText('数据中心 IP')).toBeInTheDocument();
  });

  it('provisioning node: spinner + provisioning hint, NO traffic bar', () => {
    render(
      <PrivateNodePanel node={makeNode({ status: 'provisioning', node: undefined, trafficUsedBytes: 0 })} />
    );

    expect(screen.getByText('开通中')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toBeInTheDocument(); // CircularProgress spinner
    expect(screen.queryByTestId('private-node-traffic-bar')).not.toBeInTheDocument();
    expect(screen.getByText(/节点正在开通/)).toBeInTheDocument();
  });

  it('grace node: warning chip + grace hint', () => {
    render(<PrivateNodePanel node={makeNode({ status: 'grace' })} />);

    const chip = screen.getByText('宽限期');
    expect(chip).toBeInTheDocument();
    expect(screen.getByText(/宽限期，请尽快续费/)).toBeInTheDocument();
  });

  it('suspended node: error chip + suspended hint', () => {
    render(<PrivateNodePanel node={makeNode({ status: 'suspended', node: undefined })} />);
    expect(screen.getByText('已停机')).toBeInTheDocument();
    expect(screen.getByText(/续费后恢复/)).toBeInTheDocument();
  });

  it('renew button navigates to /purchase', () => {
    navigateMock.mockClear();
    render(<PrivateNodePanel node={makeNode({})} />);
    screen.getByText('续费').click();
    expect(navigateMock).toHaveBeenCalledWith('/purchase');
  });

  it('traffic at >=95% renders error-colored bar', () => {
    render(
      <PrivateNodePanel
        node={makeNode({ trafficUsedBytes: 98 * 1024 ** 3, trafficTotalBytes: 100 * 1024 ** 3 })}
      />
    );
    const bar = screen.getByTestId('private-node-traffic-bar');
    expect(bar.getAttribute('data-color')).toBe('error');
  });

  it('quotaExhausted: renders worded exhausted alert + reset date + CTA', () => {
    render(<PrivateNodePanel node={makeNode({ quotaExhausted: true, quotaResetAt: 1_800_000_000 })} />);
    const alert = screen.getByTestId('private-node-quota-exhausted');
    expect(alert).toBeInTheDocument();
    // worded title (not the generic bar) — real i18n resolves zh-CN
    expect(within(alert).getByText('本月流量额度已用尽')).toBeInTheDocument();
    // CTA inside the alert
    expect(within(alert).getByTestId('private-node-quota-exhausted-cta')).toBeInTheDocument();
  });

  it('quotaExhausted false: no exhausted alert', () => {
    render(<PrivateNodePanel node={makeNode({ quotaExhausted: false })} />);
    expect(screen.queryByTestId('private-node-quota-exhausted')).not.toBeInTheDocument();
  });

  it('quotaExhausted suppressed while provisioning (no instance yet)', () => {
    render(
      <PrivateNodePanel
        node={makeNode({ status: 'provisioning', quotaExhausted: false, node: undefined })}
      />
    );
    expect(screen.queryByTestId('private-node-quota-exhausted')).not.toBeInTheDocument();
  });

  it('quotaExhausted CTA navigates to /purchase', () => {
    navigateMock.mockClear();
    render(<PrivateNodePanel node={makeNode({ quotaExhausted: true })} />);
    screen.getByTestId('private-node-quota-exhausted-cta').click();
    expect(navigateMock).toHaveBeenCalledWith('/purchase');
  });
});
