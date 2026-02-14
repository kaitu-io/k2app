import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConnectionButton } from '../ConnectionButton';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        connect: 'Connect',
        disconnect: 'Disconnect',
        connecting: 'Connecting...',
        connected: 'Connected',
      };
      return map[key] || key;
    },
  }),
}));

describe('ConnectionButton', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders Connect in stopped state', () => {
    render(<ConnectionButton state="stopped" onConnect={vi.fn()} onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveTextContent('Connect');
  });

  it('renders Connected in connected state', () => {
    render(<ConnectionButton state="connected" onConnect={vi.fn()} onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveTextContent('Connected');
  });

  it('is disabled during connecting', () => {
    render(<ConnectionButton state="connecting" onConnect={vi.fn()} onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button')).toBeDisabled();
  });

  it('is disabled during disconnecting', () => {
    render(<ConnectionButton state="disconnecting" onConnect={vi.fn()} onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button')).toBeDisabled();
  });

  it('calls onConnect when stopped', async () => {
    const onConnect = vi.fn();
    render(<ConnectionButton state="stopped" onConnect={onConnect} onDisconnect={vi.fn()} />);
    await userEvent.click(screen.getByRole('button'));
    expect(onConnect).toHaveBeenCalled();
  });

  it('calls onDisconnect when connected', async () => {
    const onDisconnect = vi.fn();
    render(<ConnectionButton state="connected" onConnect={vi.fn()} onDisconnect={onDisconnect} />);
    await userEvent.click(screen.getByRole('button'));
    expect(onDisconnect).toHaveBeenCalled();
  });

  it('calls onConnect when in error state', async () => {
    const onConnect = vi.fn();
    render(<ConnectionButton state="error" onConnect={onConnect} onDisconnect={vi.fn()} />);
    await userEvent.click(screen.getByRole('button'));
    expect(onConnect).toHaveBeenCalled();
  });

  it('does not call handlers during connecting', async () => {
    const onConnect = vi.fn();
    const onDisconnect = vi.fn();
    render(<ConnectionButton state="connecting" onConnect={onConnect} onDisconnect={onDisconnect} />);
    await userEvent.click(screen.getByRole('button'));
    expect(onConnect).not.toHaveBeenCalled();
    expect(onDisconnect).not.toHaveBeenCalled();
  });
});
