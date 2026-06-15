import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '../../test/utils/render';
import PrivateNodeManagement from '../PrivateNodeManagement';
import type { PrivateNodeSubscriptionView } from '../../services/api-types';

const usePrivateNodesMock = vi.fn();
vi.mock('../../hooks/usePrivateNodes', () => ({
  usePrivateNodes: () => usePrivateNodesMock(),
}));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async (orig) => {
  const actual = await orig<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => navigateMock };
});

// PrivateNodePanel renders a useNavigate — keep router from the test render wrapper.
function makeNode(id: number): PrivateNodeSubscriptionView {
  return {
    id,
    status: 'active',
    isServiceable: true,
    region: 'ap-northeast-1',
    ipType: 'non_residential',
    trafficTotalBytes: 100 * 1024 ** 3,
    trafficUsedBytes: 10 * 1024 ** 3,
    purchasedAt: 1_700_000_000,
    expiresAt: 1_800_000_000,
    graceUntil: 0,
    suspendUntil: 0,
    planLabel: `节点 ${id}`,
    node: { ip: `1.2.3.${id}`, region: 'ap-northeast-1' },
  };
}

describe('PrivateNodeManagement', () => {
  beforeEach(() => vi.clearAllMocks());

  it('loading → spinner', () => {
    usePrivateNodesMock.mockReturnValue({ nodes: [], loading: true, error: null, refresh: vi.fn() });
    render(<PrivateNodeManagement />);
    expect(screen.getByRole('progressbar')).toBeInTheDocument();
  });

  it('empty → empty text', () => {
    usePrivateNodesMock.mockReturnValue({ nodes: [], loading: false, error: null, refresh: vi.fn() });
    render(<PrivateNodeManagement />);
    expect(screen.getByText('你还没有专属节点')).toBeInTheDocument();
  });

  it('buy-line CTA navigates to /purchase?product=private_node', () => {
    usePrivateNodesMock.mockReturnValue({ nodes: [], loading: false, error: null, refresh: vi.fn() });
    render(<PrivateNodeManagement />);
    const cta = screen.getByText('购买专属线路');
    expect(cta).toBeInTheDocument();
    fireEvent.click(cta);
    expect(navigateMock).toHaveBeenCalledWith('/purchase?product=private_node');
  });

  it('non-empty → one panel per node', () => {
    usePrivateNodesMock.mockReturnValue({
      nodes: [makeNode(1), makeNode(2), makeNode(3)],
      loading: false,
      error: null,
      refresh: vi.fn(),
    });
    render(<PrivateNodeManagement />);
    expect(screen.getByText('节点 1')).toBeInTheDocument();
    expect(screen.getByText('节点 2')).toBeInTheDocument();
    expect(screen.getByText('节点 3')).toBeInTheDocument();
    // header
    expect(screen.getByText('管理专属节点')).toBeInTheDocument();
  });
});
