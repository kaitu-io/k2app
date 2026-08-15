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
const refreshOverrideNames = vi.fn();
// Mutable per-test override map — the page reads override state by app id.
let mockOverrides: Record<string, { mode: 'direct' | 'proxy'; names: string[] }> = {};
vi.mock('../../stores', () => ({
  useAppRoutesStore: (sel: any) => sel({
    overrides: mockOverrides,
    classifications: new Map([['/Applications/WeChat.app', 'direct']]),
    classifyInstalled, setOverride, refreshOverrideNames, resetOverrides, loaded: true,
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
  refreshOverrideNames.mockReset();
  mockOverrides = {};
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
  test('renders installed apps with a direct chip', async () => {
    renderPage();
    expect(await screen.findByText('WeChat')).toBeInTheDocument();
    // The intro prose also says 直连/Direct, so scope to the Chip via its class.
    const directChip = screen.getAllByText(/直连|Direct/).find((el) => el.closest('.MuiChip-root'));
    expect(directChip).toBeTruthy();
  });

  test('clicking the smart chip calls setOverride with the app', async () => {
    renderPage();
    await screen.findByText('WeChat');
    const smartChip = screen.getAllByText(/智能|Smart/).find((el) => el.closest('.MuiChip-root'));
    fireEvent.click(smartChip!);
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

  test('running rows show the executable path as a subtitle', async () => {
    // Two binaries share the basename "curl" (same process name) but live at
    // different paths. Both must render as separate rows (the engine matches by
    // name, so the path is the only thing telling them apart), and each row must
    // surface its full path. Installed apps must NOT show their bundle path.
    (window as any)._platform = {
      os: 'macos',
      appList: {
        listInstalled: vi.fn().mockResolvedValue([
          { id: '/Applications/WeChat.app', label: 'WeChat', processNames: ['WeChat'] },
        ]),
        // Both paths are outside system dirs (the native macOS pass filters
        // /usr/bin/* etc.), so both reach the webapp as separate rows.
        listRunning: vi.fn().mockResolvedValue([
          { id: '/usr/local/bin/curl', label: 'curl', processNames: ['curl'] },
          { id: '/opt/homebrew/bin/curl', label: 'curl', processNames: ['curl'] },
        ]),
      },
    };
    renderPage();
    await screen.findByText(/其他运行中的程序|Other running programs/);
    // Both same-name binaries render (dedup is by path, not name).
    expect(screen.getAllByText('curl')).toHaveLength(2);
    // Each row surfaces its own path.
    expect(screen.getByText('/usr/local/bin/curl')).toBeInTheDocument();
    expect(screen.getByText('/opt/homebrew/bin/curl')).toBeInTheDocument();
    // The installed app's bundle path is never shown.
    expect(screen.queryByText('/Applications/WeChat.app')).not.toBeInTheDocument();
  });

  test('unsupported platform shows empty state', async () => {
    (window as any)._platform = { os: 'ios', appList: undefined };
    renderPage();
    expect(await screen.findByText(/不支持|isn't supported/)).toBeInTheDocument();
  });

  // Windows: installed.id is the install DIRECTORY, running.id is the exe's
  // full path. Running exes under an installed app's directory fold into it —
  // they leave the "other running" section and their basenames ride along in
  // setOverride, so one toggle covers the whole multi-process app (F4).
  test('windows: running exes under the install dir fold into the app and extend setOverride', async () => {
    (window as any)._platform = {
      os: 'windows',
      appList: {
        listInstalled: vi.fn().mockResolvedValue([
          {
            id: 'C:\\Program Files\\Tencent\\Weixin',
            label: '微信',
            processNames: ['Weixin.exe', 'WeChatAppEx.exe'],
          },
        ]),
        listRunning: vi.fn().mockResolvedValue([
          // Depth-missed exe inside the install tree → must fold in.
          {
            id: 'C:\\Program Files\\Tencent\\Weixin\\4.1.12.55\\plugins\\WeixinUpdate.exe',
            label: 'WeixinUpdate',
            processNames: ['WeixinUpdate.exe'],
          },
          // Unrelated standalone binary → stays in "other running".
          { id: 'C:\\Tools\\rclone.exe', label: 'rclone', processNames: ['rclone.exe'] },
        ]),
      },
    };
    renderPage();
    await screen.findByText('微信');
    await screen.findByText(/其他运行中的程序|Other running programs/);
    // Folded row is claimed — not listed as "other".
    expect(screen.queryByText('WeixinUpdate')).not.toBeInTheDocument();
    expect(screen.getByText('rclone')).toBeInTheDocument();
    // Toggling the app carries the folded exe name too. Its classification
    // default is proxy (no mock map entry), so clicking 直连 sets an explicit
    // 'direct' override.
    const directChip = screen.getAllByText(/直连|Direct/).find((el) => el.closest('.MuiChip-root'));
    fireEvent.click(directChip!);
    await waitFor(() => expect(setOverride).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'C:\\Program Files\\Tencent\\Weixin',
        processNames: ['Weixin.exe', 'WeChatAppEx.exe', 'WeixinUpdate.exe'],
      }),
      'direct',
    ));
    // Stale persisted overrides get re-synced against the folded name sets.
    await waitFor(() => expect(refreshOverrideNames).toHaveBeenCalledWith([
      expect.objectContaining({
        id: 'C:\\Program Files\\Tencent\\Weixin',
        processNames: ['Weixin.exe', 'WeChatAppEx.exe', 'WeixinUpdate.exe'],
      }),
    ]));
  });

  // Override chips read back by APP ID — a shared helper basename in another
  // app's override set must not light this app's chip.
  test('override state reads by app id, not by process-name overlap', async () => {
    mockOverrides = {
      'C:\\Apps\\Other': { mode: 'direct', names: ['other.exe', 'crashpad_handler.exe'] },
    };
    (window as any)._platform = {
      os: 'windows',
      appList: {
        listInstalled: vi.fn().mockResolvedValue([
          {
            id: 'C:\\Apps\\Mine',
            label: 'Mine',
            processNames: ['mine.exe', 'crashpad_handler.exe'],
          },
        ]),
        listRunning: vi.fn().mockResolvedValue([]),
      },
    };
    renderPage();
    await screen.findByText('Mine');
    // classification default is proxy (no entry in the mock map) and there is
    // no override for THIS app id → the 直连 chip must not be highlighted.
    const directChip = screen
      .getAllByText(/直连|Direct/)
      .find((el) => el.closest('.MuiChip-root'))!
      .closest('.MuiChip-root')!;
    expect(directChip.className).not.toMatch(/colorPrimary/);
  });
});
