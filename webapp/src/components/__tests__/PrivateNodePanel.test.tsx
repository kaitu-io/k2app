import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '../../test/utils/render';
import { PrivateNodePanel } from '../PrivateNodePanel';
import type { PrivateNodeSubscriptionView } from '../../services/api-types';
// renewOnWebsite 文案含 {{brand}} 插值——断言用真实 i18n 解析，跟着当前 K2_BRAND 走
// （K2_BRAND=overleap 下同一份 zh-CN 文案会解析出 "Overleap" 而非 "开途"）。
import i18n from '../../i18n/i18n';

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

  it('不再渲染续费按钮，改为纯文字提示', () => {
    navigateMock.mockClear();
    render(<PrivateNodePanel node={makeNode({})} />);
    expect(screen.getByText(i18n.t('privateNode:privateNode.renewOnWebsite'))).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '续费' })).not.toBeInTheDocument();
    expect(navigateMock).not.toHaveBeenCalled();
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

  it('quotaExhausted: renders worded exhausted alert + reset date（无 CTA 按钮）', () => {
    render(<PrivateNodePanel node={makeNode({ quotaExhausted: true, quotaResetAt: 1_800_000_000 })} />);
    const alert = screen.getByTestId('private-node-quota-exhausted');
    expect(alert).toBeInTheDocument();
    // worded title (not the generic bar) — real i18n resolves zh-CN
    expect(within(alert).getByText('本月流量额度已用尽')).toBeInTheDocument();
    expect(screen.queryByTestId('private-node-quota-exhausted-cta')).not.toBeInTheDocument();
  });

  it('active + quotaExhausted: renewOnWebsite 提示整张卡片只出现一次（提示框内不重复，只在底部）', () => {
    render(<PrivateNodePanel node={makeNode({ status: 'active', quotaExhausted: true, quotaResetAt: 1_800_000_000 })} />);
    const alert = screen.getByTestId('private-node-quota-exhausted');
    // 额度用尽提示框里不再重复续费句——只保留标题/重置说明
    expect(within(alert).queryByText(i18n.t('privateNode:privateNode.renewOnWebsite'))).not.toBeInTheDocument();
    // 整个组件里这句话只出现一次（底部那句）
    expect(screen.getAllByText(i18n.t('privateNode:privateNode.renewOnWebsite'))).toHaveLength(1);
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

  it('quotaExhausted 不再提供跳购买页的 CTA', () => {
    navigateMock.mockClear();
    render(<PrivateNodePanel node={makeNode({ quotaExhausted: true })} />);
    expect(screen.queryByTestId('private-node-quota-exhausted-cta')).not.toBeInTheDocument();
    expect(navigateMock).not.toHaveBeenCalled();
  });
});
