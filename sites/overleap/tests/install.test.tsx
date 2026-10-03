/**
 * /install: target folding (CDN manifests + store links → four cards) and the
 * detected-platform highlight. Never a null-versioned `Overleap_null_*` link.
 */
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';
import { NAMESPACES } from '../messages/namespaces';
import { androidApkLink, buildInstallTargets, desktopLinks, type AllDownloadLinks } from '@/lib/downloads';
import { detectPlatform } from '@/lib/device-detection';

vi.mock('@/i18n/routing', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ href, children, ...rest }: any) => <a href={href} {...rest}>{children}</a>,
}));

import InstallCards from '../src/app/[locale]/install/InstallCards';

const NO_LINKS: AllDownloadLinks = { desktop: { beta: null, stable: null }, mobile: null };

describe('buildInstallTargets', () => {
  it('nothing published → every platform is "coming soon" (empty url)', () => {
    const targets = buildInstallTargets(NO_LINKS);
    expect(targets.map((t) => t.platform)).toEqual(['windows', 'macos', 'ios', 'android']);
    expect(targets.every((t) => t.url === '')).toBe(true);
    expect(JSON.stringify(targets)).not.toContain('null');
  });

  it('desktop artifacts use the CDN layout and Overleap_ prefix; stable wins over beta', () => {
    const all: AllDownloadLinks = {
      desktop: { beta: { version: '0.5.0-beta.1', links: desktopLinks('0.5.0-beta.1') }, stable: { version: '0.4.10', links: desktopLinks('0.4.10') } },
      mobile: null,
    };
    const targets = buildInstallTargets(all);
    expect(targets[0]).toEqual({ platform: 'windows', url: 'https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/0.4.10/Overleap_0.4.10_x64.exe', version: '0.4.10' });
    expect(targets[1].url).toBe('https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/0.4.10/Overleap_0.4.10_universal.pkg');
  });

  it('store listings win over CDN manifests; without Play the APK is offered', () => {
    const withManifest: AllDownloadLinks = {
      ...NO_LINKS,
      mobile: { ios: { url: 'https://apps.apple.com/app/id1', version: '1' }, android: { url: androidApkLink('1.2.3'), version: '1.2.3' } },
    };
    const fromManifest = buildInstallTargets(withManifest);
    expect(fromManifest[2]).toMatchObject({ platform: 'ios', url: 'https://apps.apple.com/app/id1', store: true });
    expect(fromManifest[3]).toMatchObject({ url: 'https://d13jc1jqzlg4yt.cloudfront.net/overleap/android/1.2.3/Overleap-1.2.3.apk', store: false });
    const fromStores = buildInstallTargets(withManifest, { ios: 'https://apps.apple.com/app/id9', android: 'https://play.google.com/store/apps/details?id=x' });
    expect(fromStores[2].url).toBe('https://apps.apple.com/app/id9');
    expect(fromStores[3]).toMatchObject({ url: 'https://play.google.com/store/apps/details?id=x', store: true });
  });
});

describe('detectPlatform', () => {
  it.each([
    ['Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)', 'ios'],
    ['Mozilla/5.0 (Linux; Android 14; Pixel 8)', 'android'],
    ['Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'windows'],
    ['Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'macos'],
    ['Mozilla/5.0 (X11; Linux x86_64)', null],
  ])('%s → %s', (ua, expected) => {
    expect(detectPlatform(ua)).toBe(expected);
  });
});

describe('InstallCards', () => {
  const messages = Object.fromEntries(
    NAMESPACES.map((ns) => [ns, JSON.parse(fs.readFileSync(path.resolve(__dirname, '../messages/en-GB', `${ns}.json`), 'utf8'))]),
  );

  it('highlights and moves the detected platform first; unavailable cards are disabled', async () => {
    vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
    const targets = buildInstallTargets({ desktop: { beta: null, stable: { version: '0.4.10', links: desktopLinks('0.4.10') } }, mobile: null });
    render(
      <NextIntlClientProvider locale="en-GB" messages={messages} onError={(e) => { throw e; }}>
        <InstallCards targets={targets} />
      </NextIntlClientProvider>,
    );
    expect((await screen.findByTestId('detected-platform')).textContent).toBe("You're on Mac");
    const cards = screen.getAllByTestId(/^install-card-/);
    expect(cards[0].getAttribute('data-testid')).toBe('install-card-macos');
    expect(within(cards[0]).getByRole('link').getAttribute('href')).toContain('Overleap_0.4.10_universal.pkg');
    expect(within(cards[0]).getByText('Version 0.4.10')).toBeInTheDocument();
    const ios = screen.getByTestId('install-card-ios');
    expect(within(ios).getByRole('button', { name: 'Coming soon' })).toBeDisabled();
  });
});
