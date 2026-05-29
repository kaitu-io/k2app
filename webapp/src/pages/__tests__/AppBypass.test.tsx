import { describe, test, expect, vi, beforeEach, beforeAll } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
// Project render wraps children in I18nextProvider so t() returns real
// translations (not raw keys) — the badge assertions match the zh/en strings.
import { render } from '../../test/utils/render';
import { i18nPromise } from '../../i18n/i18n';
import AppBypass from '../AppBypass';

const setOverride = vi.fn();
const resetOverrides = vi.fn();
const classifyInstalled = vi.fn();
vi.mock('../../stores', () => ({
  useAppRoutesStore: (sel: any) => sel({
    forceProxy: [], forceDirect: [],
    classifications: new Map([['/Applications/WeChat.app', 'direct']]),
    classifyInstalled, setOverride, resetOverrides, loaded: true,
  }),
  useVPNMachineStore: (sel: any) => sel({ state: 'idle' }),
  useConfigStore: (sel: any) => sel({ country: 'cn', resolvePreset: () => 'bypass' }),
}));

beforeAll(async () => {
  // Ensure i18n resources are loaded before any render (init is async).
  await i18nPromise;
});

beforeEach(() => {
  setOverride.mockReset();
  classifyInstalled.mockReset();
  (window as any)._platform = {
    os: 'macos',
    appList: {
      listInstalled: vi.fn().mockResolvedValue([
        { id: '/Applications/WeChat.app', label: 'WeChat', processNames: ['WeChat'] },
      ]),
      listRunning: vi.fn().mockResolvedValue([]),
    },
  };
});

function renderPage() {
  return render(<AppBypass />);
}

describe('AppBypass page', () => {
  test('renders installed apps with a default-direct badge', async () => {
    renderPage();
    expect(await screen.findByText('WeChat')).toBeInTheDocument();
    expect(screen.getByText(/默认直连|Direct by default/)).toBeInTheDocument();
  });

  test('clicking force-proxy calls setOverride with the app', async () => {
    renderPage();
    await screen.findByText('WeChat');
    fireEvent.click(screen.getByText(/强制代理|Force proxy/));
    await waitFor(() => expect(setOverride).toHaveBeenCalledWith(
      expect.objectContaining({ id: '/Applications/WeChat.app', processNames: ['WeChat'] }), 'proxy'));
  });

  test('unsupported platform shows empty state', async () => {
    (window as any)._platform = { os: 'ios', appList: undefined };
    renderPage();
    expect(await screen.findByText(/不支持|isn't supported/)).toBeInTheDocument();
  });
});
