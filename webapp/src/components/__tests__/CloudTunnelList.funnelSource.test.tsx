/**
 * Funnel: the locked-tunnel renew CTA navigates to /purchase with
 * from=tunnel_locked. Mock style follows CloudTunnelList.test.tsx.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import { useConnectionStore } from '../../stores/connection.store';
import i18n from '../../i18n/i18n';

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: { isAuthenticated: boolean }) => unknown) => selector({ isAuthenticated: true }),
}));
vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachineStore: (selector: (s: { state: string }) => unknown) => selector({ state: 'idle' }),
}));
vi.mock('../../services/cache-store', () => ({
  cacheStore: { get: vi.fn(() => null), set: vi.fn(), delete: vi.fn() },
}));
const mockCloudApiGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: (...a: unknown[]) => mockCloudApiGet(...a) },
}));
vi.mock('../../utils/country', () => ({ getCountryName: (c: string) => c, getFlagIcon: (c: string) => c }));
vi.mock('../RecommendBar', () => ({ RecommendBar: () => <div /> }));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

describe('CloudTunnelList → /purchase carries its funnel source', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useConnectionStore.setState({ cloudAccessRevoked: false });
    (window as any)._platform = { os: 'macos', version: '0.4.10' };
  });

  it('renew CTA on the membership-expired state navigates with from=tunnel_locked', async () => {
    mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });
    render(<CloudTunnelList selectedDomain={null} onSelect={vi.fn()} />);
    const cta = await screen.findByText(i18n.t('dashboard:dashboard.renewMembership'));
    fireEvent.click(cta);
    await waitFor(() =>
      expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'tunnel_locked' } }),
    );
  });
});
