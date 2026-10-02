import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import CookieConsent, { COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION } from '../CookieConsent';
import { COOKIE_BANNER_OFFSET_VAR } from '@/lib/cookie-banner';

const brandState = vi.hoisted(() => ({ gaMeasurementId: '' }));
vi.mock('@/hooks/useBrand', () => ({ useBrand: () => brandState }));

const OPTOUT = '/api/px/optout';

describe('CookieConsent', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    window.localStorage.clear();
    brandState.gaMeasurementId = '';
    window.history.pushState({}, '', '/zh-CN');
    fetchMock = vi.fn(() => Promise.resolve(new Response(null)));
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  const show = () => {
    render(<CookieConsent />);
    act(() => { vi.advanceTimersByTime(1600); });
  };
  const settle = () => act(() => { vi.advanceTimersByTime(400); });

  it('decline asks the server to opt this browser out of the statistics', () => {
    show();
    fireEvent.click(screen.getByText('discovery.cookieConsent.decline'));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(OPTOUT, { credentials: 'same-origin', redirect: 'manual' });
    settle();
    expect(window.localStorage.getItem(COOKIE_CONSENT_KEY)).toBe(COOKIE_CONSENT_VERSION);
    expect(screen.queryByText('discovery.cookieConsent.decline')).toBeNull();
  });

  it('a failing opt-out request never surfaces', () => {
    fetchMock.mockImplementation(() => Promise.reject(new Error('offline')));
    show();
    expect(() => fireEvent.click(screen.getByText('discovery.cookieConsent.decline'))).not.toThrow();
    settle();
    expect(screen.queryByText('discovery.cookieConsent.decline')).toBeNull();
  });

  it('accept does not opt out', () => {
    show();
    fireEvent.click(screen.getByText('discovery.cookieConsent.accept'));
    settle();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(window.localStorage.getItem(COOKIE_CONSENT_KEY)).toBe(COOKIE_CONSENT_VERSION);
  });

  it('a visitor who answered the previous version sees the new text once', () => {
    expect(COOKIE_CONSENT_VERSION).not.toBe('1');
    window.localStorage.setItem(COOKIE_CONSENT_KEY, '1');
    show();
    expect(screen.getByText('discovery.cookieConsent.details')).toBeTruthy();
  });

  it('stays hidden once the current version is answered', () => {
    window.localStorage.setItem(COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION);
    show();
    expect(screen.queryByText('discovery.cookieConsent.details')).toBeNull();
  });

  it('stays hidden in embed mode', () => {
    window.history.pushState({}, '', '/zh-CN/releases?embed=true');
    show();
    expect(screen.queryByText('discovery.cookieConsent.details')).toBeNull();
  });

  // The layout loads a third-party analytics script only for a brand whose
  // registry entry has a measurement id; the banner says so for exactly that brand.
  it('a brand with a measurement id also discloses the third-party analytics cookies', () => {
    brandState.gaMeasurementId = 'G-TEST';
    show();
    expect(screen.getByText('discovery.cookieConsent.details')).toBeTruthy();
    expect(screen.getByText('discovery.cookieConsent.detailsThirdParty')).toBeTruthy();
  });

  it('a brand without a measurement id does not', () => {
    show();
    expect(screen.getByText('discovery.cookieConsent.details')).toBeTruthy();
    expect(screen.queryByText('discovery.cookieConsent.detailsThirdParty')).toBeNull();
  });
  // The chat launcher sits in the same corner; it reads this variable to stay above the banner.
  describe('publishes how much of the bottom edge it covers', () => {
    const offset = () => document.documentElement.style.getPropertyValue(COOKIE_BANNER_OFFSET_VAR);
    let height: ReturnType<typeof vi.spyOn>;
    beforeEach(() => {
      height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(212);
    });
    afterEach(() => {
      height.mockRestore();
      document.documentElement.style.removeProperty(COOKIE_BANNER_OFFSET_VAR);
    });

    it('nothing before the banner appears; its height + bottom gap while shown; cleared once answered', () => {
      render(<CookieConsent />);
      expect(offset()).toBe('');
      act(() => { vi.advanceTimersByTime(1600); });
      const banner = screen.getByText('discovery.cookieConsent.details').closest('.fixed') as HTMLElement;
      banner.style.bottom = '24px';
      act(() => { window.dispatchEvent(new Event('resize')); });
      expect(offset()).toBe('236px');
      fireEvent.click(screen.getByText('discovery.cookieConsent.accept'));
      settle();
      expect(offset()).toBe('');
    });

    it('never set when the banner does not show (already answered)', () => {
      window.localStorage.setItem(COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION);
      show();
      expect(offset()).toBe('');
    });

    it('cleared when the banner unmounts while still showing', () => {
      const { unmount } = render(<CookieConsent />);
      act(() => { vi.advanceTimersByTime(1600); });
      expect(offset()).toBe('212px');
      unmount();
      expect(offset()).toBe('');
    });
  });
});
