import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import ConnectedSettingsLock from '../ConnectedSettingsLock';
import { useVPNMachineStore } from '../../stores';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const translations: Record<string, string> = {
        'dashboard:dashboard.advancedSettingsLocked': 'VPN 已连接，请先断开后再修改高级设置',
      };
      return translations[key] ?? key;
    },
  }),
  // i18n/i18n.ts runs initI18n() on module load via i18n.use(initReactI18next);
  // omitting this export causes an unhandled rejection that fails vitest with exit 1.
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

beforeEach(() => {
  useVPNMachineStore.setState({ state: 'idle' } as any);
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
  });
});
