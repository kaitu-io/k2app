import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';

const mockUseEmbedMode = vi.fn(() => ({
  isEmbedded: false,
  showNavigation: true,
  showFooter: true,
  compactLayout: false,
  authToken: null as string | null,
  embedTheme: null as 'auto' | 'light' | 'dark' | null,
}));

vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => mockUseEmbedMode(),
}));

import BrowserWarningBar from '../BrowserWarningBar';

const CHROME_DESKTOP =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';
const WECHAT_ANDROID =
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MicroMessenger/8.0.32.2300';
const SAFARI_IOS_12 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 12_5_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1';
const SAFARI_IOS_17 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1';

function spoofUA(ua: string) {
  Object.defineProperty(window.navigator, 'userAgent', { value: ua, configurable: true });
}

const originalUA = window.navigator.userAgent;

beforeEach(() => {
  mockUseEmbedMode.mockReturnValue({
    isEmbedded: false,
    showNavigation: true,
    showFooter: true,
    compactLayout: false,
    authToken: null,
    embedTheme: null,
  });
});

afterEach(() => {
  spoofUA(originalUA);
});

describe('BrowserWarningBar', () => {
  it('does not render for mainstream browsers (Chrome desktop)', async () => {
    spoofUA(CHROME_DESKTOP);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('renders the warning bar for WeChat Android', async () => {
    spoofUA(WECHAT_ANDROID);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    expect(alert.textContent).toContain('common.browserWarning.message');
  });

  it('renders the outdated-iOS warning for iOS 12 Safari (mainstream UA but unsupported version)', async () => {
    spoofUA(SAFARI_IOS_12);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    expect(alert.textContent).toContain('common.browserWarning.outdatedIos');
  });

  it('does not render for iOS 17 Safari (current version)', async () => {
    spoofUA(SAFARI_IOS_17);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('prefers outdated-iOS reason over in-app-webview when both apply', async () => {
    const wechatOnIOS12 = 'Mozilla/5.0 (iPhone; CPU iPhone OS 12_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40';
    spoofUA(wechatOnIOS12);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('common.browserWarning.outdatedIos');
  });

  it('does not render in embed mode even with a webview UA', async () => {
    spoofUA(WECHAT_ANDROID);
    mockUseEmbedMode.mockReturnValue({
      isEmbedded: true,
      showNavigation: false,
      showFooter: false,
      compactLayout: true,
      authToken: 'token-stub',
      embedTheme: 'auto',
    });
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
