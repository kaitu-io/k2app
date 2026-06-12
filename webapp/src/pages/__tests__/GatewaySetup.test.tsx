import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '../../test/utils/render';
import GatewaySetup from '../GatewaySetup';

const setCredMock = vi.fn();
vi.mock('../../services/gateway-core', () => ({
  gatewaySetCredential: (url: string) => setCredMock(url),
}));

describe('GatewaySetup', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setCredMock.mockResolvedValue({ code: 0 });
  });

  it('submits pasted url to set-credential', async () => {
    render(<GatewaySetup />);
    fireEvent.change(screen.getByTestId('setup-url-input'), {
      target: { value: 'k2subs://u:t@h/api/subs' },
    });
    screen.getByTestId('setup-submit').click();
    await waitFor(() =>
      expect(setCredMock).toHaveBeenCalledWith('k2subs://u:t@h/api/subs'),
    );
  });

  it('shows success alert on code 0', async () => {
    render(<GatewaySetup />);
    fireEvent.change(screen.getByTestId('setup-url-input'), {
      target: { value: 'k2subs://u:t@h/api/subs' },
    });
    screen.getByTestId('setup-submit').click();
    await waitFor(() =>
      expect(screen.getByTestId('setup-ok')).toBeInTheDocument(),
    );
  });

  it('shows error alert on non-zero code', async () => {
    setCredMock.mockResolvedValue({ code: 1, message: 'bad url' });
    render(<GatewaySetup />);
    fireEvent.change(screen.getByTestId('setup-url-input'), {
      target: { value: 'not-a-url' },
    });
    screen.getByTestId('setup-submit').click();
    await waitFor(() =>
      expect(screen.getByTestId('setup-err')).toBeInTheDocument(),
    );
  });
});
