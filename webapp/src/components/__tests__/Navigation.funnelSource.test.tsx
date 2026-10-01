/**
 * Funnel: the navigation entries to /purchase identify themselves as `nav`
 * (Purchase maps router state `from` to the paywall_view source). Other
 * destinations navigate without state.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/Navigation.funnelSource.test.tsx
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});
vi.mock('../../hooks/useUser', () => ({ useUser: () => ({ user: null }) }));
vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) => selector({ isAuthenticated: true }),
}));

import BottomNavigation from '../BottomNavigation';
import SideNavigation from '../SideNavigation';
import { PAYWALL_SOURCES } from '../../services/funnel-events';

const wrap = (ui: React.ReactElement) =>
  render(
    <MemoryRouter initialEntries={['/']}>
      <I18nextProvider i18n={i18n}>{ui}</I18nextProvider>
    </MemoryRouter>,
  );

describe('navigation entries to /purchase carry from=nav', () => {
  beforeEach(() => { navigateMock.mockReset(); });

  it('nav is a registered paywall source', () => {
    expect(PAYWALL_SOURCES).toContain('nav');
  });

  it('BottomNavigation: purchase tab', () => {
    wrap(<BottomNavigation />);
    fireEvent.click(screen.getByText(i18n.t('nav:navigation.purchase')));
    expect(navigateMock).toHaveBeenCalledTimes(1);
    expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'nav' } });
  });

  it('BottomNavigation: other tabs navigate without state', () => {
    wrap(<BottomNavigation />);
    fireEvent.click(screen.getByText(i18n.t('nav:navigation.account')));
    expect(navigateMock).toHaveBeenCalledWith('/account');
  });

  it('SideNavigation: purchase entry', () => {
    wrap(<SideNavigation />);
    fireEvent.click(screen.getByText(i18n.t('nav:navigation.purchase')));
    expect(navigateMock).toHaveBeenCalledTimes(1);
    expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'nav' } });
  });

  it('SideNavigation: other entries navigate without state', () => {
    wrap(<SideNavigation />);
    fireEvent.click(screen.getByText(i18n.t('nav:navigation.account')));
    expect(navigateMock).toHaveBeenCalledWith('/account');
  });
});
