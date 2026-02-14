import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ServerList } from '../ServerList';
import type { Server } from '../../stores/servers.store';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'common:loading': 'Loading...',
        'common:servers': 'Servers',
      };
      return map[key] || key;
    },
  }),
}));

const mockServers: Server[] = [
  {
    id: 'sv-1',
    name: 'Tokyo #1',
    country: 'Japan',
    countryCode: 'JP',
    city: 'Tokyo',
    wireUrl: 'wg://tokyo-1.example.com',
    load: 42,
  },
  {
    id: 'sv-2',
    name: 'Singapore #1',
    country: 'Singapore',
    countryCode: 'SG',
    wireUrl: 'wg://sg-1.example.com',
    load: 78,
  },
  {
    id: 'sv-3',
    name: 'US West #1',
    country: 'United States',
    countryCode: 'US',
    city: 'Los Angeles',
    wireUrl: 'wg://us-west-1.example.com',
  },
];

describe('ServerList', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders server items with names and countries', () => {
    render(
      <ServerList servers={mockServers} selectedId={null} onSelect={vi.fn()} />
    );

    expect(screen.getByText('Tokyo #1')).toBeInTheDocument();
    expect(screen.getByText('Tokyo, Japan')).toBeInTheDocument();

    expect(screen.getByText('Singapore #1')).toBeInTheDocument();
    expect(screen.getByText('Singapore')).toBeInTheDocument();

    expect(screen.getByText('US West #1')).toBeInTheDocument();
    expect(screen.getByText('Los Angeles, United States')).toBeInTheDocument();
  });

  it('highlights selected server', () => {
    render(
      <ServerList
        servers={mockServers}
        selectedId="sv-2"
        onSelect={vi.fn()}
      />
    );

    const buttons = screen.getAllByRole('button');
    // The selected button (sv-2, index 1) should have blue styling
    expect(buttons[1]!.className).toContain('bg-blue-50');
    // Non-selected buttons should not
    expect(buttons[0]!.className).not.toContain('bg-blue-50');
    expect(buttons[2]!.className).not.toContain('bg-blue-50');
  });

  it('calls onSelect when clicked', async () => {
    const onSelect = vi.fn();
    render(
      <ServerList servers={mockServers} selectedId={null} onSelect={onSelect} />
    );

    await userEvent.click(screen.getByText('Singapore #1'));

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(mockServers[1]);
  });

  it('shows loading message when empty', () => {
    render(
      <ServerList servers={[]} selectedId={null} onSelect={vi.fn()} />
    );

    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('shows server load percentage when available', () => {
    render(
      <ServerList servers={mockServers} selectedId={null} onSelect={vi.fn()} />
    );

    expect(screen.getByText('42%')).toBeInTheDocument();
    expect(screen.getByText('78%')).toBeInTheDocument();
  });
});
