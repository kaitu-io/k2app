import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '../../test/utils/render';
import { AddRouterCard } from '../AddRouterCard';

const discoverMock = vi.fn();
vi.mock('../../services/private-node-service', () => ({
  discoverRouter: () => discoverMock(),
}));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

let mockPhase = 'unconfigured';
vi.mock('../../stores/router.store', () => ({
  useRouterStore: (sel: any) => sel({ phase: mockPhase }),
}));

describe('AddRouterCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPhase = 'unconfigured';
    discoverMock.mockResolvedValue([{ lanIP: '192.168.8.1', port: 1779 }]);
  });

  it('shows nothing beyond title/intro when no candidates are discovered', async () => {
    discoverMock.mockResolvedValue([]);
    render(<AddRouterCard />);
    await waitFor(() => expect(discoverMock).toHaveBeenCalled());
    expect(screen.queryByTestId('add-router-manage')).not.toBeInTheDocument();
  });

  it('shows the manage button once a router candidate is discovered', async () => {
    render(<AddRouterCard />);
    await waitFor(() =>
      expect(screen.getByTestId('add-router-manage')).toBeInTheDocument(),
    );
  });

  it('manage button navigates to the Router tab', async () => {
    render(<AddRouterCard />);
    const button = await waitFor(() => screen.getByTestId('add-router-manage'));
    button.click();
    expect(navigateMock).toHaveBeenCalledWith('/router');
  });

  it('shows the legacy-firmware upgrade hint when the anchor is unreachable (phase=none)', async () => {
    mockPhase = 'none';
    render(<AddRouterCard />);
    await waitFor(() =>
      expect(screen.getByTestId('add-router-legacy-hint')).toBeInTheDocument(),
    );
  });

  it('does not show the upgrade hint once the anchor is reachable', async () => {
    mockPhase = 'online';
    render(<AddRouterCard />);
    await waitFor(() =>
      expect(screen.getByTestId('add-router-manage')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('add-router-legacy-hint')).not.toBeInTheDocument();
  });
});
