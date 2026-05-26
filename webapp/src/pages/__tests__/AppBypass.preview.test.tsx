/**
 * AppBypass — preview section render + visibility gates.
 *
 * Covers:
 *  - Empty state (0 matches, region set, featureSupported=true)
 *  - Populated state (2 matched entries with hit_kind labels)
 *  - Hidden when region is empty
 *  - Hidden when featureSupported is false
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import AppBypass from '../AppBypass';
import { useAppBypassStore } from '../../stores/app-bypass.store';
import { useConfigStore } from '../../stores/config.store';
import { useVPNMachineStore } from '../../stores/vpn-machine.store';
import { mockDaemonBackedPlatform, mockK2Run } from '../../test/utils/platform-mock';

function renderPage() {
  return render(<AppBypass />, { useMemoryRouter: true });
}

/** Stable k2 mock: returns an empty preview for app-bypass-preview; ignores other actions. */
function stubK2() {
  mockK2Run(async (action: string) => {
    if (action === 'app-bypass-preview') {
      return { code: 0, message: 'ok', data: { region: 'cn', matched: [] } };
    }
    if (action === 'app-bypass-get') {
      return {
        code: 0, message: 'ok', data: {
          feature_supported: true, region: 'cn', custom: { process: [], package: [] },
        },
      };
    }
    return { code: 0, message: 'ok', data: null };
  });
}

describe('AppBypass preview section', () => {
  beforeEach(() => {
    // VPN machine must be idle so the page-level guard doesn't redirect.
    useVPNMachineStore.setState({ state: 'idle' });

    // Daemon-backed platform (enables preview section).
    mockDaemonBackedPlatform();

    // Stub _k2.run so mount effects don't throw unhandled rejections.
    stubK2();

    // Store state: preview-eligible baseline (featureSupported=true, region='cn', no matches).
    useAppBypassStore.setState({
      entries: [],
      loaded: true,
      candidates: [],
      candidatesLoadedAt: Date.now(),
      candidatesLoading: false,
      candidatesError: null,
      matched: [],
      matchedLoadedAt: 0,
      matchedLoading: false,
      matchedError: null,
      featureSupported: true,
      region: 'cn',
    });

    // Config store: country='cn' (lowercase, matches Select options), bypass preset → smartBypassActive=true.
    useConfigStore.setState({ country: 'cn', countryVia: 'direct', defaultVia: 'proxy' });
  });

  it('renders the section heading with count=0 when no matches', async () => {
    renderPage();
    // dashboard:appBypass.preview.section with count=0
    // zh-CN: "智能识别命中（0）", en-US: "Detected apps (0)"
    const heading = await screen.findByText(/智能识别命中（0）|Detected apps \(0\)/);
    expect(heading).toBeTruthy();
  });

  it('shows empty-state message when matched is empty and not loading', async () => {
    renderPage();
    // dashboard:appBypass.preview.empty
    // zh-CN: "当前未匹配到任何应用", en-US: "No installed apps matched"
    const msg = await screen.findByText(/当前未匹配到|No installed apps matched/);
    expect(msg).toBeTruthy();
  });

  it('renders matched entries with hit_kind label and hit_pattern', () => {
    useAppBypassStore.setState({
      matched: [
        { id: 'a', label: 'WeChat', names: ['wechat'], hit_kind: 'process_prefix', hit_pattern: 'wechat' },
        { id: 'b', label: 'GTJA', names: ['com.gtja.client'], hit_kind: 'package_exact', hit_pattern: 'com.gtja.client' },
      ],
    });
    renderPage();

    expect(screen.getByText('WeChat')).toBeTruthy();
    expect(screen.getByText('GTJA')).toBeTruthy();

    // dashboard:appBypass.preview.hitKind.process_prefix + " — wechat"
    // zh-CN: "进程前缀匹配 — wechat", en-US: "Process prefix match — wechat"
    expect(screen.getByText(/进程前缀匹配 — wechat|Process prefix match — wechat/)).toBeTruthy();

    // dashboard:appBypass.preview.hitKind.package_exact + " — com.gtja.client"
    // zh-CN: "包名精确匹配 — com\.gtja\.client", en-US: "Package name match — com\.gtja\.client"
    expect(screen.getByText(/包名精确匹配 — com\.gtja\.client|Package name match — com\.gtja\.client/)).toBeTruthy();
  });

  it('hides section when region is empty', () => {
    useAppBypassStore.setState({ region: '' });
    renderPage();
    expect(screen.queryByText(/智能识别命中|Detected apps/)).toBeNull();
  });

  it('hides section when featureSupported is false', () => {
    useAppBypassStore.setState({ featureSupported: false });
    renderPage();
    expect(screen.queryByText(/智能识别命中|Detected apps/)).toBeNull();
  });
});
