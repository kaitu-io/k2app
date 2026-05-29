import { describe, test, expect, vi, beforeEach, beforeAll } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
// Project render wraps children in I18nextProvider so t() returns real
// translations (not raw keys) — the chip assertions match the zh/en strings.
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
  test('renders installed apps with a default-direct chip', async () => {
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

  test('running list dedups by process name, not id, and shows only the supplement', async () => {
    // A running app whose id differs from the installed app (macOS: running.id
    // is the bundle identifier, installed.id is the bundle path) but shares a
    // process name — must NOT reappear. A standalone binary with no installed
    // counterpart must show under "其他运行中的程序".
    (window as any)._platform = {
      os: 'macos',
      appList: {
        listInstalled: vi.fn().mockResolvedValue([
          { id: '/Applications/WeChat.app', label: 'WeChat', processNames: ['WeChat'] },
        ]),
        listRunning: vi.fn().mockResolvedValue([
          { id: 'com.tencent.xinWeChat', label: 'WeChat', processNames: ['WeChat'] },
          { id: '/opt/homebrew/bin/node', label: 'node', processNames: ['node'] },
        ]),
      },
    };
    renderPage();
    await screen.findByText(/其他运行中的程序|Other running programs/);
    expect(screen.getByText('node')).toBeInTheDocument();
    // WeChat appears exactly once (installed section only — not duplicated).
    expect(screen.getAllByText('WeChat')).toHaveLength(1);
  });

  test('unsupported platform shows empty state', async () => {
    (window as any)._platform = { os: 'ios', appList: undefined };
    renderPage();
    expect(await screen.findByText(/不支持|isn't supported/)).toBeInTheDocument();
  });
});
