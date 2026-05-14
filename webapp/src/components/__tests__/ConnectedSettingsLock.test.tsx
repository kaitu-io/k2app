import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ConnectedSettingsLock from '../ConnectedSettingsLock';
import { useVPNMachineStore, useConnectionStore } from '../../stores';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const translations: Record<string, string> = {
        'dashboard:dashboard.advancedSettingsLocked': 'VPN 已连接，请先断开后再修改高级设置',
        'dashboard:dashboard.disconnectVpn': '断开 VPN',
      };
      return translations[key] ?? key;
    },
  }),
}));

const mockDisconnect = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  useVPNMachineStore.setState({ state: 'idle' } as any);
  useConnectionStore.setState({ disconnect: mockDisconnect } as any);
  mockDisconnect.mockClear();
});

describe('ConnectedSettingsLock', () => {
  it('renders children unmodified when vpnState is idle', () => {
    render(<ConnectedSettingsLock><div data-testid="child">x</div></ConnectedSettingsLock>);
    const child = screen.getByTestId('child');
    expect(child).toBeInTheDocument();
    expect(child.parentElement?.style.pointerEvents).toBe('');
  });

  it('renders Alert + locks pointer events when not idle', () => {
    useVPNMachineStore.setState({ state: 'connected' } as any);
    render(<ConnectedSettingsLock><div data-testid="child">x</div></ConnectedSettingsLock>);
    expect(screen.getByText(/请先断开/)).toBeInTheDocument();
    expect(screen.getByText(/断开 VPN/)).toBeInTheDocument();
  });

  it('clicking 断开 VPN calls connection.disconnect (which drives _k2.run(down))', () => {
    useVPNMachineStore.setState({ state: 'connected' } as any);
    render(<ConnectedSettingsLock><div /></ConnectedSettingsLock>);
    fireEvent.click(screen.getByText(/断开 VPN/));
    expect(mockDisconnect).toHaveBeenCalledTimes(1);
  });
});
