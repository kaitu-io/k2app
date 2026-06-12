import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '../../test/utils/render';
import { AddRouterCard } from '../AddRouterCard';

const mintMock = vi.fn();
const discoverMock = vi.fn();
vi.mock('../../services/private-node-service', () => ({
  mintGatewayCredential: () => mintMock(),
  discoverRouter: () => discoverMock(),
}));

describe('AddRouterCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mintMock.mockResolvedValue('k2subs://u:t@h/api/subs');
    discoverMock.mockResolvedValue([{ lanIP: '192.168.8.1', port: 1779 }]);
  });

  it('mint button reveals the k2subs url', async () => {
    render(<AddRouterCard />);
    screen.getByTestId('add-router-mint').click();
    await waitFor(() =>
      expect(screen.getByTestId('add-router-url')).toHaveTextContent('k2subs://u:t@h/api/subs'),
    );
  });

  it('shows discovered router open link', async () => {
    render(<AddRouterCard />);
    await waitFor(() => {
      const link = screen.getByTestId('add-router-open-0') as HTMLAnchorElement;
      expect(link.getAttribute('href')).toBe('http://192.168.8.1:1779');
    });
  });
});
