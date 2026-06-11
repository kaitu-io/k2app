import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { usePrivateNodes } from '../usePrivateNodes';
import { cacheStore } from '../../services/cache-store';
import type { PrivateNodeSubscriptionView } from '../../services/api-types';

const getPrivateNodesMock = vi.fn();
vi.mock('../../services/private-node-service', () => ({
  getPrivateNodes: () => getPrivateNodesMock(),
}));

// Logged-in auth store
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: { isAuthenticated: boolean }) => unknown) =>
    selector({ isAuthenticated: true }),
}));

const activeNode: PrivateNodeSubscriptionView = {
  id: 1,
  status: 'active',
  isServiceable: true,
  region: 'ap-northeast-1',
  ipType: 'non_residential',
  trafficTotalBytes: 2 * 1024 ** 4,
  trafficUsedBytes: 1024 ** 4,
  purchasedAt: 1_700_000_000,
  expiresAt: 1_800_000_000,
  graceUntil: 0,
  suspendUntil: 0,
  planLabel: '专属节点测试',
  node: { ip: '1.2.3.4', region: 'ap-northeast-1' },
};

describe('usePrivateNodes', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    cacheStore.clear();
    // Re-establish default resolved value after clearAllMocks wipes it.
    getPrivateNodesMock.mockResolvedValue({ items: [activeNode] });
  });

  it('populates nodes from the service and clears loading', async () => {
    const { result } = renderHook(() => usePrivateNodes());

    expect(result.current.loading).toBe(true);

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.nodes).toHaveLength(1);
    expect(result.current.nodes[0].id).toBe(1);
    expect(getPrivateNodesMock).toHaveBeenCalledTimes(1);
  });

  it('refresh() re-fetches from the service', async () => {
    const { result } = renderHook(() => usePrivateNodes());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(getPrivateNodesMock).toHaveBeenCalledTimes(1);

    getPrivateNodesMock.mockResolvedValueOnce({ items: [] });
    await act(async () => {
      await result.current.refresh();
    });

    await waitFor(() => expect(result.current.nodes).toHaveLength(0));
    expect(getPrivateNodesMock).toHaveBeenCalledTimes(2);
  });

  it('exposes error when the service rejects', async () => {
    getPrivateNodesMock.mockRejectedValueOnce(new Error('boom'));
    const { result } = renderHook(() => usePrivateNodes());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeTruthy();
    expect(result.current.nodes).toHaveLength(0);
  });
});
